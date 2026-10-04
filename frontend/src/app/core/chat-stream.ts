import {
  ChatConfirmEvent,
  ChatDoneEvent,
  ChatMessage,
  ChatTextEvent,
  ChatToolCall,
  ChatToolResultEvent,
  ChatUiEvent,
  Problem,
} from '../api/models';
import { CHAT_ROLE } from '../api/models/chat-role-array';
import { CHAT_TURN_END } from '../api/models/chat-turn-end-array';

/** One event of a `text/event-stream`: its name, and its data lines joined by line feeds. */
export interface StreamedEvent {
  name: string;
  data: string;
}

/**
 * Cuts a `text/event-stream` into its events as the text arrives, by the event stream format of
 * the HTML standard: a line ends with CRLF, LF or CR — a CR that ends a piece waits for the next
 * one, which may begin with its LF —, a blank line ends an event, a line that begins with a colon
 * is a comment (the `: keep-alive` of a chat turn), the `data` lines of an event are joined by
 * line feeds, and `id`, `retry` and unknown fields are read past. An event without data is none,
 * and neither is what follows the last blank line when the stream ends.
 */
export class EventStreamParser {
  private rest = '';
  private name = '';
  private data: string[] = [];

  /** The events this piece of the stream completes, in order. */
  push(piece: string): StreamedEvent[] {
    let text = this.rest + piece;
    let held = '';
    if (text.endsWith('\r')) {
      held = '\r';
      text = text.slice(0, -1);
    }
    const lines = text.split(/\r\n|\r|\n/);
    this.rest = (lines.pop() as string) + held;
    const events: StreamedEvent[] = [];
    for (const line of lines) {
      const event = this.line(line);
      if (event) {
        events.push(event);
      }
    }
    return events;
  }

  private line(line: string): StreamedEvent | undefined {
    if (line === '') {
      const event =
        this.data.length > 0
          ? { name: this.name || 'message', data: this.data.join('\n') }
          : undefined;
      this.name = '';
      this.data = [];
      return event;
    }
    if (line.startsWith(':')) {
      return undefined;
    }
    const colon = line.indexOf(':');
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? '' : line.slice(colon + 1).replace(/^ /, '');
    if (field === 'event') {
      this.name = value;
    } else if (field === 'data') {
      this.data.push(value);
    }
    return undefined;
  }
}

/** The events of a chat turn (docs/adr/0076), each with its data as the API document has it. */
export type ChatStreamEvent =
  | { name: 'text'; data: ChatTextEvent }
  | { name: 'tool_call'; data: ChatToolCall }
  | { name: 'ui'; data: ChatUiEvent }
  | { name: 'tool_result'; data: ChatToolResultEvent }
  | { name: 'confirm'; data: ChatConfirmEvent }
  /** The turn failed once it had begun; the problem, or undefined where the data is none. */
  | { name: 'error'; data: Problem | undefined }
  | { name: 'done'; data: ChatDoneEvent };

type Fields = Record<string, unknown>;

/** A JSON object, or undefined for anything else. */
function object(value: unknown): Fields | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Fields)
    : undefined;
}

function parsed(data: string): Fields | undefined {
  try {
    return object(JSON.parse(data));
  } catch {
    return undefined;
  }
}

function strings(fields: Fields, ...keys: string[]): boolean {
  return keys.every((key) => typeof fields[key] === 'string');
}

/** A call as the model made it: an id, a tool's name and its arguments, a JSON object. */
function call(fields: Fields | undefined): ChatToolCall | undefined {
  const args = object(fields?.['arguments']);
  return fields && args && strings(fields, 'id', 'name')
    ? { id: fields['id'] as string, name: fields['name'] as string, arguments: args }
    : undefined;
}

function problem(fields: Fields | undefined): Problem | undefined {
  return fields && typeof fields['code'] === 'string' && typeof fields['status'] === 'number'
    ? (fields as unknown as Problem)
    : undefined;
}

/** `done`: the messages to append, each an object of a role the conversation knows, and why. */
function done(fields: Fields | undefined): ChatDoneEvent | undefined {
  const messages = fields?.['messages'];
  const reason = fields?.['reason'];
  const known = (message: unknown) =>
    CHAT_ROLE.includes(object(message)?.['role'] as ChatMessage['role']);
  return Array.isArray(messages) &&
    messages.every(known) &&
    CHAT_TURN_END.includes(reason as ChatDoneEvent['reason'])
    ? { messages: messages as ChatMessage[], reason: reason as ChatDoneEvent['reason'] }
    : undefined;
}

/**
 * A streamed event as a chat event; undefined for an event the client does not know, or whose
 * data is not what the API document says — the client reads past it. An `error` is a failure
 * whatever its data says.
 */
export function chatEvent(event: StreamedEvent): ChatStreamEvent | undefined {
  const fields = parsed(event.data);
  switch (event.name) {
    case 'text':
      return fields && strings(fields, 'delta')
        ? { name: 'text', data: { delta: fields['delta'] as string } }
        : undefined;
    case 'tool_call': {
      const data = call(fields);
      return data ? { name: 'tool_call', data } : undefined;
    }
    case 'ui':
      return fields && fields['action'] === 'navigate' && strings(fields, 'path')
        ? { name: 'ui', data: { action: 'navigate', path: fields['path'] as string } }
        : undefined;
    case 'tool_result':
      return fields && strings(fields, 'id', 'summary') && typeof fields['ok'] === 'boolean'
        ? {
            name: 'tool_result',
            data: {
              id: fields['id'] as string,
              ok: fields['ok'] as boolean,
              summary: fields['summary'] as string,
            },
          }
        : undefined;
    case 'confirm': {
      const proposed = call(fields);
      return proposed && strings(fields as Fields, 'description')
        ? {
            name: 'confirm',
            data: { ...proposed, description: (fields as Fields)['description'] as string },
          }
        : undefined;
    }
    case 'error':
      return { name: 'error', data: problem(fields) };
    case 'done': {
      const data = done(fields);
      return data ? { name: 'done', data } : undefined;
    }
    default:
      return undefined;
  }
}

/**
 * The chat events of a response body as they arrive. A character whose bytes are split between
 * two pieces waits for its rest. What fails the body — the person's Stop, a cut connection — is
 * thrown as it comes; leaving the loop early cancels the body, which closes the connection.
 */
export async function* chatEvents(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ChatStreamEvent, void, undefined> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  const parser = new EventStreamParser();
  try {
    for (;;) {
      const { done: ended, value } = await reader.read();
      if (ended) {
        return;
      }
      for (const event of parser.push(decoder.decode(value, { stream: true }))) {
        const typed = chatEvent(event);
        if (typed) {
          yield typed;
        }
      }
    }
  } finally {
    reader.cancel().catch(() => undefined);
  }
}
