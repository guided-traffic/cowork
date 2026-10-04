package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

const entityAttachment = "attachment"

// uploadBudget is the memory the uploads in flight may hold together; the
// backend runs with a 256Mi limit and no writable disk, so an upload is
// buffered in memory, at most COWORK_ATTACHMENT_MAX_BYTES + 1 bytes each.
const uploadBudget = 64 << 20

// uploadSlots is how many uploads are buffered at once: at least one.
func uploadSlots(maxBytes int64) int {
	if maxBytes <= 0 || maxBytes >= uploadBudget {
		return 1
	}
	return int(uploadBudget / maxBytes)
}

// attachment is the columns every attachment query returns.
type attachment = readq.GetAttachmentRow

func attachmentView(t tenantScope, tc ticketCtx, a attachment) apigen.Attachment {
	return apigen.Attachment{
		Id: a.ID, FileName: a.FileName, Size: a.Size, Sha256: hex.EncodeToString(a.Sha256),
		ContentType: apigen.AttachmentContentType(a.ContentType), Comment: nullableOf(a.CommentID),
		UploadedBy: personView(a.UploadedBy, a.UploadedByUsername, a.UploadedByName), Agent: nullableOf(a.Agent),
		Token: tokenMarkView(a.TokenID, a.TokenName), CreatedAt: a.CreatedAt,
		ContentUrl: ticketURL(t, tc.project.Key, tc.row.Number) + "/attachments/" + a.ID.String() + "/content",
	}
}

// upload is a read multipart upload.
type upload struct {
	data    []byte
	name    string
	comment *uuid.UUID
}

// readUpload reads the one file and the optional comment_id of an upload,
// the file buffered up to the maximum and one byte more — or whole, when the
// maximum is 0 (docs/adr/0039 D2).
func readUpload(mr *multipart.Reader, maxBytes int64) (upload, *problem.Error) {
	var u upload
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if perr := partError(err, maxBytes); perr != nil {
			return u, perr
		}
		switch part.FormName() {
		case "file":
			if u.data != nil {
				return u, problem.Field("/file", "one file per upload")
			}
			u.name = part.FileName()
			var r io.Reader = part
			if maxBytes > 0 {
				r = io.LimitReader(part, maxBytes+1)
			}
			u.data, err = io.ReadAll(r)
			if perr := partError(err, maxBytes); perr != nil {
				return u, perr
			}
			if maxBytes > 0 && int64(len(u.data)) > maxBytes {
				return u, tooLarge(maxBytes)
			}
		case "comment_id":
			v, err := io.ReadAll(io.LimitReader(part, 64))
			id, perr := uuid.Parse(strings.TrimSpace(string(v)))
			if err != nil || perr != nil {
				return u, problem.Field("/comment_id", "not a comment id")
			}
			u.comment = &id
		default:
			return u, problem.Field("/"+part.FormName(), "an upload takes file and comment_id only")
		}
	}
	if u.data == nil {
		return u, problem.Field("/file", "the upload has no file")
	}
	return u, nil
}

// partError names a failed read: the body beyond its limit is 413,
// anything else a malformed upload.
func partError(err error, maxBytes int64) *problem.Error {
	var tooBig *http.MaxBytesError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &tooBig):
		return tooLarge(maxBytes)
	case errors.Is(err, os.ErrDeadlineExceeded):
		return problem.New(problem.Timeout, "the upload did not arrive within the request timeout")
	}
	return problem.New(problem.ValidationFailed, "the upload is not a readable multipart body")
}

// uploadNeed is an upload: a member's act with write scope; an agent needs
// upload (docs/adr/0043 D4).
var uploadNeed = auth.Need{Role: domain.RoleMember, Scope: domain.ScopeWrite, Capability: auth.CapUpload}

// file is an upload read and judged: its bytes, sanitised name, stored type
// and hash.
type file struct {
	upload
	contentType string
	sum         [sha256.Size]byte
}

// UploadAttachment stores a file on a ticket or one of its comments
// (docs/adr/0016): the type from the bytes, the metadata row and the object
// in one act — a failed put rolls the row back, a failed commit removes the
// object.
func (s *Server) UploadAttachment(ctx context.Context, req apigen.UploadAttachmentRequestObject) (apigen.UploadAttachmentResponseObject, error) {
	t := tenantFrom(ctx)
	if s.storage == nil {
		return nil, problem.New(problem.UploadsDisabled, "this installation stores no attachments")
	}
	if perr := auth.Authorize(principal(ctx), t.Role, uploadNeed); perr != nil {
		return nil, perr
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	f, perr := s.readFile(req.Body)
	if perr != nil {
		return nil, perr
	}
	ctx, perr = s.keyed(ctx, req.Params.IdempotencyKey, "uploadAttachment", fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number),
		map[string]any{"sha256": hex.EncodeToString(f.sum[:]), "name": f.name, "comment": f.comment})
	if perr != nil {
		return nil, perr
	}
	key := storage.Key(t.ID, uuid.Must(uuid.NewV7()))
	var out apigen.Attachment
	put := false
	replay, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		var err error
		out, put, err = s.storeFile(ctx, w, t, req, f, key)
		return err
	})
	if put && (err != nil || replay != nil) {
		// The row did not commit, or a concurrent request with the same key
		// did: no row names these bytes.
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
	}
	if err != nil {
		return nil, err
	}
	if replay != nil {
		body, err := replayed[apigen.Attachment](replay)
		if err != nil {
			return nil, err
		}
		return apigen.UploadAttachment201JSONResponse{Body: body, Headers: apigen.UploadAttachment201ResponseHeaders{Location: header(replay, headerLocation)}}, nil
	}
	return apigen.UploadAttachment201JSONResponse{Body: out, Headers: apigen.UploadAttachment201ResponseHeaders{Location: &out.ContentUrl}}, nil
}

// readFile reads the upload and judges its bytes (docs/adr/0016 D3).
func (s *Server) readFile(body *multipart.Reader) (file, *problem.Error) {
	u, perr := readUpload(body, s.h.opts.AttachmentMaxBytes)
	if perr != nil {
		return file{}, perr
	}
	contentType, detected, ok := domain.DetectAttachmentType(u.data)
	if !ok {
		return file{}, problem.New(problem.UnsupportedMediaType, "the file is "+detected+", which cowork does not store")
	}
	u.name = domain.FileNameFor(domain.SanitizeFileName(u.name), contentType)
	return file{upload: u, contentType: contentType, sum: sha256.Sum256(u.data)}, nil
}

// storeFile writes the metadata row with its act, then the object under
// key; put reports whether the object was written.
func (s *Server) storeFile(ctx context.Context, w *store.Writer, t tenantScope, req apigen.UploadAttachmentRequestObject, f file, key string) (apigen.Attachment, bool, error) {
	tc, err := visibleTicket(ctx, w.Reader, t, req.Project, req.Number)
	if err != nil {
		return apigen.Attachment{}, false, err
	}
	if err := w.LockAttachments(ctx, tc.row.ID); err != nil {
		return apigen.Attachment{}, false, err
	}
	if err := s.mayAttach(ctx, w.Reader, t, tc, uploadNeed, f.comment); err != nil {
		return apigen.Attachment{}, false, err
	}
	id := uuid.MustParse(key[strings.LastIndex(key, "/")+1:])
	p := principal(ctx)
	ins := writeq.InsertAttachmentParams{ID: id, TenantID: t.ID, TicketID: tc.row.ID, CommentID: f.comment, FileName: f.name,
		Size: int64(len(f.data)), Sha256: f.sum[:], ContentType: f.contentType, UploadedBy: p.PersonID}
	ins.TokenID, ins.TokenName = actToken(p)
	if p.IsAgent() {
		ins.Agent = &p.Agent
	}
	if err := w.InsertAttachment(ctx, ins); err != nil {
		return apigen.Attachment{}, false, fmt.Errorf("insert the attachment: %w", err)
	}
	w.Record(store.Event{EntityType: entityAttachment, EntityID: id, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
		Action: "uploaded", After: map[string]any{"file_name": f.name, "size": len(f.data), "content_type": f.contentType,
			"sha256": hex.EncodeToString(f.sum[:])}})
	if err := s.storage.Put(ctx, key, bytes.NewReader(f.data), int64(len(f.data)), f.contentType); err != nil {
		return apigen.Attachment{}, false, err
	}
	a, err := w.GetAttachment(ctx, readq.GetAttachmentParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id})
	if err != nil {
		return apigen.Attachment{}, true, err
	}
	out := attachmentView(t, tc, a)
	res, err := stored(out, map[string]string{headerLocation: out.ContentUrl})
	if err != nil {
		return out, true, err
	}
	w.Respond(res)
	return out, true, nil
}

// mayAttach holds an upload to the ticket's role, the comment it names —
// one of the ticket's, by the caller's person — and the per-ticket count.
func (s *Server) mayAttach(ctx context.Context, r *store.Reader, t tenantScope, tc ticketCtx, need auth.Need, comment *uuid.UUID) error {
	if perr := auth.Authorize(principal(ctx), tc.role, need); perr != nil {
		return perr
	}
	if comment != nil {
		c, err := r.GetCommentForWrite(ctx, readq.GetCommentForWriteParams{TenantID: t.ID, TicketID: tc.row.ID, ID: *comment})
		if errors.Is(err, pgx.ErrNoRows) {
			return problem.Field("/comment_id", "no such comment on this ticket")
		}
		if err != nil {
			return err
		}
		if c.AuthorID != principal(ctx).PersonID {
			return problem.New(problem.Forbidden, "a comment's attachments are its author's")
		}
	}
	if max := s.h.opts.AttachmentMaxPerTicket; max > 0 {
		n, err := r.CountAttachments(ctx, readq.CountAttachmentsParams{TenantID: t.ID, TicketID: tc.row.ID})
		if err != nil {
			return err
		}
		if n >= int64(max) {
			return problem.New(problem.AttachmentLimit, fmt.Sprintf("the ticket holds %d attachments, the most it takes", n))
		}
	}
	return nil
}

// visibleAttachment reads an attachment through its ticket's predicate.
func visibleAttachment(ctx context.Context, r *store.Reader, t tenantScope, project string, number int, id uuid.UUID) (ticketCtx, attachment, error) {
	tc, err := visibleTicket(ctx, r, t, project, number)
	if err != nil {
		return tc, attachment{}, err
	}
	a, err := r.GetAttachment(ctx, readq.GetAttachmentParams{TenantID: t.ID, TicketID: tc.row.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return tc, a, problem.New(problem.NotFound, "no such attachment")
	}
	return tc, a, err
}

// ListAttachments lists a ticket's attachments.
func (s *Server) ListAttachments(ctx context.Context, req apigen.ListAttachmentsRequestObject) (apigen.ListAttachmentsResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	const op = "listAttachments"
	scope := fmt.Sprintf("%s/%s/%d", t.ID, req.Project, req.Number)
	size := s.h.pageSize(req.Params.Limit)
	var tc ticketCtx
	var rows []readq.ListAttachmentsRow
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		if tc, err = visibleTicket(ctx, r, t, req.Project, req.Number); err != nil {
			return err
		}
		after, err := s.uuidAfter(op, scope, req.Params.Cursor)
		if err != nil {
			return err
		}
		rows, err = r.ListAttachments(ctx, readq.ListAttachmentsParams{TenantID: t.ID, TicketID: tc.row.ID, After: after, PageSize: limitArg(size)})
		return err
	})
	if err != nil {
		return nil, err
	}
	rows, next := page(s.h, rows, size, op, scope, func(a readq.ListAttachmentsRow) string { return a.ID.String() })
	out := apigen.ListAttachments200JSONResponse{Items: make([]apigen.Attachment, 0, len(rows)), NextCursor: nullableString(next)}
	for _, a := range rows {
		out.Items = append(out.Items, attachmentView(t, tc, attachment(a)))
	}
	return out, nil
}

// GetAttachment answers an attachment's metadata.
func (s *Server) GetAttachment(ctx context.Context, req apigen.GetAttachmentRequestObject) (apigen.GetAttachmentResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var tc ticketCtx
	var a attachment
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		tc, a, err = visibleAttachment(ctx, r, t, req.Project, req.Number, req.Attachment)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetAttachment200JSONResponse(attachmentView(t, tc, a)), nil
}

// DownloadAttachment streams an attachment's bytes after the ticket's read
// check, with headers that make the browser treat them as data
// (docs/adr/0016 D4, D5). Every 200 is recorded as data leaving the system
// (docs/adr/0026 D5).
func (s *Server) DownloadAttachment(ctx context.Context, req apigen.DownloadAttachmentRequestObject) (apigen.DownloadAttachmentResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, read); perr != nil {
		return nil, perr
	}
	var tc ticketCtx
	var a attachment
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		var err error
		tc, a, err = visibleAttachment(ctx, r, t, req.Project, req.Number, req.Attachment)
		return err
	})
	if err != nil {
		return nil, err
	}
	tag := `"` + hex.EncodeToString(a.Sha256) + `"`
	if notModified(req.Params.IfNoneMatch, tag) {
		return apigen.DownloadAttachment304Response{Headers: apigen.DownloadAttachment304ResponseHeaders{ETag: &tag}}, nil
	}
	if s.storage == nil {
		return nil, problem.New(problem.NotFound, "this installation has no object storage; the attachment's bytes are not reachable")
	}
	body, size, err := s.storage.Get(ctx, storage.Key(t.ID, a.ID))
	if errors.Is(err, storage.ErrMissing) {
		return nil, problem.New(problem.NotFound, "the attachment's bytes are missing from storage; a restore may have brought the database back without them")
	}
	if err != nil {
		return nil, err
	}
	_, err = s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		w.Record(store.Event{EntityType: entityAttachment, EntityID: a.ID, TicketID: tc.row.ID, TicketKey: ticketKey(t, tc.row),
			Action: "downloaded", After: map[string]any{"file_name": a.FileName}})
		return nil
	})
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	disposition := "attachment"
	if domain.InlineAttachment(a.ContentType) {
		disposition = "inline"
	}
	disposition = mime.FormatMediaType(disposition, map[string]string{"filename": a.FileName})
	nosniff, sandbox := "nosniff", "sandbox"
	return apigen.DownloadAttachment200AsteriskResponse{Body: body, ContentType: a.ContentType, ContentLength: size,
		Headers: apigen.DownloadAttachment200ResponseHeaders{ETag: &tag, ContentDisposition: &disposition,
			XContentTypeOptions: &nosniff, ContentSecurityPolicy: &sandbox}}, nil
}
