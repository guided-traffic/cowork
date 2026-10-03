import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import {
  addComment,
  answerQuestion,
  askQuestion,
  linkTickets,
  removeInterest,
  setInterest,
  unlinkTickets,
  withdrawQuestion,
} from '../api/functions';
import { Comment, InterestWeight, LinkType, Question, QuestionCreate } from '../api/models';
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

  comment(key: string, body: string): Promise<Comment> {
    return this.api.invoke(addComment, {
      ...routeOf(key),
      'Idempotency-Key': crypto.randomUUID(),
      body: { body },
    });
  }

  ask(key: string, question: QuestionCreate): Promise<Question> {
    return this.api.invoke(askQuestion, {
      ...routeOf(key),
      'Idempotency-Key': crypto.randomUUID(),
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
