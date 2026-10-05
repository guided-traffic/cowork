import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import {
  addComment,
  answerQuestion,
  askQuestion,
  editComment,
  linkTickets,
  listCommentRevisions,
  removeInterest,
  setInterest,
  unlinkTickets,
  updateQuestion,
  withdrawComment,
  withdrawQuestion,
} from '../api/functions';
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

  /** Links this ticket, as the source, to another of the tenant by its short key. */
  link(key: string, type: LinkType, other: string): Promise<unknown> {
    return this.api.invoke(linkTickets, { ...routeOf(key), type, other });
  }

  /** Removes a link from its source's side: `source` and `target` are canonical keys. */
  unlink(source: string, type: LinkType, target: string): Promise<unknown> {
    const short = target.slice(target.indexOf('/') + 1);
    return this.api.invoke(unlinkTickets, { ...routeOf(source), type, other: short });
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
