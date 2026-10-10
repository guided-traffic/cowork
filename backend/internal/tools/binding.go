package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// Situation is what the working directory and the server say about the
// session's binding (docs/adr/0066 D3, D4).
type Situation struct {
	// Binding is the binding; nil when the session runs unbound.
	Binding *Binding
	// Remotes are the repository's remotes, origin first.
	Remotes []Remote
	// Lookup is the server's answer for the remotes; nil without remotes.
	Lookup *apigen.RepositoryLookup
	// File is the .cowork.yaml that applies.
	File *BindingFile
	// Notes are what the session is told about how it was bound or why not:
	// a file of another installation, a file that names nothing, a
	// repository git cannot read.
	Notes []string
}

// Resolve finds the session's binding: the server's binding of the remotes,
// or a .cowork.yaml, which wins when it is present and whose disagreement
// with the server is reported as drift (docs/adr/0066 D3, D4,
// docs/adr/0006 D3). The binding found is the session's from then on.
func Resolve(ctx context.Context, s *Session) (Situation, error) {
	var sit Situation
	if s.Workspace == nil {
		return sit, usage("this host has no working directory to read a binding from")
	}
	path := readWorkspace(ctx, s, &sit)
	if len(sit.Remotes) > 0 {
		var err error
		if sit.Lookup, err = lookup(ctx, s, sit.Remotes, path); err != nil {
			return sit, err
		}
	}
	if sit.File != nil {
		if err := fileBinding(ctx, s, &sit, sit.File); err != nil {
			return sit, err
		}
	} else {
		remoteBinding(&sit)
	}
	s.setBinding(sit.Binding)
	return sit, nil
}

// readWorkspace reads the binding file that applies and the remotes, and
// returns the sub-directory the lookup asks about: the file's, or the working
// directory's.
func readWorkspace(ctx context.Context, s *Session, sit *Situation) string {
	file, err := s.Workspace.BindingFile(ctx)
	if err != nil {
		sit.Notes = append(sit.Notes, "The binding file is unusable and is ignored: "+err.Error())
	}
	if file != nil && file.URL != "" && !sameInstallation(file.URL, s.Installation) {
		sit.Notes = append(sit.Notes, fmt.Sprintf("%s belongs to the installation %s, not to this one; it is ignored.", file.File, file.URL))
		file = nil
	}
	sit.File = file
	if sit.Remotes, err = s.Workspace.Remotes(ctx); err != nil {
		sit.Notes = append(sit.Notes, "The git remotes could not be read: "+err.Error())
	}
	if file != nil {
		return file.Path
	}
	path, err := s.Workspace.Path(ctx)
	if err != nil {
		return ""
	}
	return path
}

// remoteBinding binds the session by the server's one binding of a remote.
func remoteBinding(sit *Situation) {
	if sit.Lookup == nil || sit.Lookup.Status != apigen.RepositoryLookupStatusBound {
		return
	}
	b := sit.Lookup.Bindings[0]
	sit.Binding = &Binding{Team: b.Team.Slug, Project: b.Project.Key, ProjectName: b.Project.Name, Source: sourceRemote,
		Remote: remoteOf(*sit, b.Identity), Identity: b.Identity, Path: b.Path}
	if b.Archived {
		sit.Notes = append(sit.Notes, "The project "+sit.Binding.Key()+" is archived: it keeps its tickets and refuses new ones.")
	}
}

// lookup asks the server which project binds the remotes (docs/adr/0066 D2).
func lookup(ctx context.Context, s *Session, remotes []Remote, path string) (*apigen.RepositoryLookup, error) {
	params := &apigen.LookupRepositoryParams{}
	for _, r := range remotes {
		if len(params.Remote) == 10 {
			break
		}
		params.Remote = append(params.Remote, r.URL)
	}
	if path != "" {
		params.Path = &path
	}
	res, err := s.API.LookupRepositoryWithResponse(ctx, params)
	if err := check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

// fileBinding binds the session to the file's project once the project is
// found, and reports where the server says otherwise.
func fileBinding(ctx context.Context, s *Session, sit *Situation, file *BindingFile) error {
	res, err := s.API.GetProjectWithResponse(ctx, file.Team, file.Project)
	if err == nil && res.StatusCode() == http.StatusNotFound {
		sit.Notes = append(sit.Notes, fmt.Sprintf("%s binds %s/%s, a project that does not exist or that you cannot see; the session runs unbound.",
			file.File, file.Team, file.Project))
		return nil
	}
	if err := check(res, err, http.StatusOK); err != nil {
		return err
	}
	b := &Binding{Team: file.Team, Project: file.Project, ProjectName: res.JSON200.Name, Source: sourceFile, Path: file.Path}
	if res.JSON200.ArchivedAt.IsSpecified() && !res.JSON200.ArchivedAt.IsNull() {
		sit.Notes = append(sit.Notes, "The project "+b.Key()+" is archived: it keeps its tickets and refuses new ones.")
	}
	if l := sit.Lookup; l != nil && len(l.Bindings) > 0 {
		var others []string
		for _, sb := range l.Bindings {
			if sb.Team.Slug != b.Team || sb.Project.Key != b.Project {
				others = append(others, sb.Team.Slug+"/"+sb.Project.Key)
			}
		}
		if len(others) > 0 {
			b.Drift = fmt.Sprintf("the server binds %s to %s, %s binds %s; the file wins for this session (docs/adr/0066 D4)",
				l.Bindings[0].Identity, strings.Join(others, " and "), file.File, b.Key())
		}
	}
	sit.Binding = b
	return nil
}

// remoteOf is the remote whose identity the lookup matched.
func remoteOf(sit Situation, identity string) string {
	if sit.Lookup == nil {
		return ""
	}
	for i, r := range sit.Lookup.Remotes {
		if id, err := r.Identity.Get(); err == nil && id == identity && i < len(sit.Remotes) {
			return sit.Remotes[i].Name
		}
	}
	return ""
}

// sameInstallation compares two installation URLs by scheme, host, port and
// path.
func sameInstallation(a, b string) bool {
	norm := func(s string) string {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil {
			return s
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/")
	}
	return norm(a) == norm(b)
}
