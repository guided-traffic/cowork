import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { addComment } from '../api/fn/comments/add-comment';
import { editComment } from '../api/fn/comments/edit-comment';
import { listCommentRevisions } from '../api/fn/comments/list-comment-revisions';
import { withdrawComment } from '../api/fn/comments/withdraw-comment';
import { answerQuestion } from '../api/fn/questions/answer-question';
import { askQuestion } from '../api/fn/questions/ask-question';
import { updateQuestion } from '../api/fn/questions/update-question';
import { withdrawQuestion } from '../api/fn/questions/withdraw-question';
import { linkTicketTo } from '../api/fn/tickets/link-ticket-to';
import { removeInterest } from '../api/fn/tickets/remove-interest';
import { removeTicketChild } from '../api/fn/tickets/remove-ticket-child';
import { removeTicketLink } from '../api/fn/tickets/remove-ticket-link';
import { setInterest } from '../api/fn/tickets/set-interest';
import {
  Comment,
  CommentRevision,
  InterestWeight,
  LinkType,
  Question,
  QuestionCreate,
  QuestionPatch,
} from '../api/models';
import { etagOf } from './entity-cache';
import { routeOf } from './ticket-actions.service';
import { splitKey } from './tickets.service';

/**
 * The writes around a ticket (docs/adr/0018 D2): comments (docs/adr/0015), questions and their
 * answers (docs/adr/0011), links (docs/adr/0012) and the person's stake (docs/adr/0013). What they
 * change reaches the page through the event stream, which reloads the part that changed.
 */
@Injectable({ providedIn: 'root' })
export class Conversation {
  private readonly api = inject(Api);

  /**
   * Comments on the ticket, mentioning the persons of `mentions` by id (docs/adr/0015 D5). The
   * idempotency key is the form's, one for each content it holds, so a retry of a lost answer is
   * answered again instead of commenting twice (docs/adr/0045 D3); so is the one of a question.
   */
  comment(
    key: string,
    body: string,
    idempotencyKey: string,
    mentions: string[] = [],
  ): Promise<Comment> {
    return this.api.invoke(addComment, {
      ...routeOf(key),
      'Idempotency-Key': idempotencyKey,
      body: mentions.length > 0 ? { body, mentions } : { body },
    });
  }

  /**
   * Replaces a comment's text over the version the editing began with (docs/adr/0015 D3,
   * docs/adr/0050 D3): the author's act; the previous text is kept in its history. `mentions`
   * replaces the comment's, and tells the persons it adds (D5); left out, they stay.
   */
  editComment(key: string, comment: Comment, body: string, mentions?: string[]): Promise<Comment> {
    return this.api.invoke(editComment, {
      ...routeOf(key),
      comment: comment.id,
      'If-Match': etagOf(comment.version),
      body: mentions ? { body, mentions } : { body },
    });
  }

  /** A comment's previous texts, oldest first; none once it is withdrawn. */
  async commentRevisions(key: string, comment: Comment): Promise<CommentRevision[]> {
    const list = await this.api.invoke(listCommentRevisions, {
      ...routeOf(key),
      comment: comment.id,
      limit: 200,
    });
    return list.items;
  }

  /** Hides a comment's text and keeps its entry: its author's act, or a tenant administrator's. */
  withdrawComment(key: string, comment: Comment): Promise<Comment> {
    return this.api.invoke(withdrawComment, { ...routeOf(key), comment: comment.id });
  }

  ask(key: string, question: QuestionCreate, idempotencyKey: string): Promise<Question> {
    return this.api.invoke(askQuestion, {
      ...routeOf(key),
      'Idempotency-Key': idempotencyKey,
      body: question,
    });
  }

  /** Answers an open question; changing an answer overwrites it and sends the version read (docs/adr/0050 D3). */
  answer(key: string, question: Question, answer: string): Promise<Question> {
    return this.api.invoke(answerQuestion, {
      ...routeOf(key),
      question: question.number,
      ...(question.status === 'answered' ? { 'If-Match': etagOf(question.version) } : {}),
      body: { answer },
    });
  }

  /** Changes an open question's text over the version the editing began with: the asker's act. */
  editQuestion(key: string, question: Question, patch: QuestionPatch): Promise<Question> {
    return this.api.invoke(updateQuestion, {
      ...routeOf(key),
      question: question.number,
      'If-Match': etagOf(question.version),
      body: patch,
    });
  }

  withdraw(key: string, question: Question): Promise<Question> {
    return this.api.invoke(withdrawQuestion, { ...routeOf(key), question: question.number });
  }

  /**
   * Links this ticket, as the source, to another by its canonical key, a ticket of any team the
   * person reads (docs/adr/0012 D2); one they do not read is the `404` of one that does not exist.
   */
  link(key: string, type: LinkType, other: string): Promise<unknown> {
    const { team, key: short } = splitKey(other);
    return this.api.invoke(linkTicketTo, { ...routeOf(key), type, other_team: team, other: short });
  }

  /**
   * Removes a link of the ticket `key` by the link's id — the ticket either end of it, its source or
   * its target, whatever team keeps the link and whatever the person reads of the other end
   * (docs/adr/0012 D2 as amended again 2026-10-10); the id reaches a link whose other end shows no
   * key (docs/adr/0065 D5). A link that is gone already is `404` "no such link".
   */
  unlink(key: string, link: string): Promise<unknown> {
    return this.api.invoke(removeTicketLink, { ...routeOf(key), link });
  }

  /**
   * Detaches a child of any project or team from the ticket `key`, its parent, by the id its
   * relation carries — an opaque handle (docs/adr/0008 D2 as amended again 2026-10-10). A child
   * that is gone already is `404` "no such child".
   */
  removeChild(key: string, child: string): Promise<unknown> {
    return this.api.invoke(removeTicketChild, { ...routeOf(key), child });
  }

  setInterest(key: string, weight: InterestWeight, note: string): Promise<unknown> {
    return this.api.invoke(setInterest, {
      ...routeOf(key),
      body: { weight, ...(note.trim() ? { note: note.trim() } : {}) },
    });
  }

  removeInterest(key: string): Promise<unknown> {
    return this.api.invoke(removeInterest, routeOf(key));
  }
}
