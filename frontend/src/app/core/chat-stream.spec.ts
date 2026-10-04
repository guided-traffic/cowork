import { chatEvent, chatEvents, ChatStreamEvent, EventStreamParser } from './chat-stream';

/** A response body that hands out the pieces a test gives it, as a network would. */
class Body {
  private controller!: ReadableStreamDefaultController<Uint8Array>;
  cancelled = false;
  readonly stream = new ReadableStream<Uint8Array>({
    start: (controller) => {
      this.controller = controller;
    },
    cancel: () => {
      this.cancelled = true;
    },
  });

  send(text: string): void {
    this.bytes(new TextEncoder().encode(text));
  }

  bytes(bytes: Uint8Array): void {
    this.controller.enqueue(bytes);
  }

  end(): void {
    this.controller.close();
  }

  fail(error: unknown): void {
    this.controller.error(error);
  }
}

/** Reads every event of the body until it ends, or until it fails — the failure is returned. */
async function readAll(body: Body): Promise<{ events: ChatStreamEvent[]; failure?: unknown }> {
  const events: ChatStreamEvent[] = [];
  try {
    for await (const event of chatEvents(body.stream)) {
      events.push(event);
    }
    return { events };
  } catch (failure) {
    return { events, failure };
  }
}

const frame = (name: string, data: unknown) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;

describe('EventStreamParser', () => {
  let parser: EventStreamParser;

  beforeEach(() => {
    parser = new EventStreamParser();
  });

  it('reads an event, its name and its data', () => {
    expect(parser.push('event: text\ndata: {"delta":"Hi"}\n\n')).toEqual([
      { name: 'text', data: '{"delta":"Hi"}' },
    ]);
  });

  it('reads several events of one piece in order', () => {
    expect(parser.push('event: a\ndata: 1\n\nevent: b\ndata: 2\n\n')).toEqual([
      { name: 'a', data: '1' },
      { name: 'b', data: '2' },
    ]);
  });

  it('reads an event cut into pieces at every place, the same as in one piece', () => {
    const stream = 'event: tool_call\ndata: {"id":"c1","name":"get_ticket","arguments":{}}\n\n';
    for (let cut = 1; cut < stream.length; cut++) {
      const pieces = new EventStreamParser();

      const events = [...pieces.push(stream.slice(0, cut)), ...pieces.push(stream.slice(cut))];

      expect(events).toEqual([
        { name: 'tool_call', data: '{"id":"c1","name":"get_ticket","arguments":{}}' },
      ]);
    }
  });

  it('completes nothing until the blank line that ends the event', () => {
    expect(parser.push('event: text\n')).toEqual([]);
    expect(parser.push('data: {"delta":"a"}\n')).toEqual([]);
    expect(parser.push('\n')).toEqual([{ name: 'text', data: '{"delta":"a"}' }]);
  });

  it.each([
    ['CRLF', '\r\n'],
    ['CR', '\r'],
    ['LF', '\n'],
  ])('ends a line with %s', (_name, end) => {
    // A CR that ends a piece may be the first half of a CRLF: the next piece tells.
    expect([
      ...parser.push(`event: text${end}data: x${end}${end}`),
      ...parser.push('event: next'),
    ]).toEqual([{ name: 'text', data: 'x' }]);
  });

  it('takes a CRLF cut between two pieces for one line end, not two', () => {
    expect(parser.push('event: text\r\ndata: x\r')).toEqual([]);
    expect(parser.push('\n\r')).toEqual([]);
    expect(parser.push('\n')).toEqual([{ name: 'text', data: 'x' }]);
  });

  it('takes a CR at the end of a piece for a line end when no LF follows', () => {
    expect(parser.push('data: x\r')).toEqual([]);
    expect(parser.push('\r')).toEqual([]);
    expect(parser.push('event: next\n')).toEqual([{ name: 'message', data: 'x' }]);
  });

  it('reads past the keep-alive comment and every other comment', () => {
    expect(
      parser.push(': keep-alive\n\n:\n\nevent: text\n: in the middle\ndata: x\n\n: keep-alive\n\n'),
    ).toEqual([{ name: 'text', data: 'x' }]);
  });

  it('joins the data lines of an event with line feeds', () => {
    expect(parser.push('event: text\ndata: one\ndata:\ndata: three\n\n')).toEqual([
      { name: 'text', data: 'one\n\nthree' },
    ]);
  });

  it('takes the value after the colon with one space removed, and only one', () => {
    expect(parser.push('event:text\ndata:x\n\nevent: text\ndata:  y\n\n')).toEqual([
      { name: 'text', data: 'x' },
      { name: 'text', data: ' y' },
    ]);
  });

  it('keeps every colon of the value after the first', () => {
    expect(parser.push('data: {"a":"b:c"}\n\n')).toEqual([
      { name: 'message', data: '{"a":"b:c"}' },
    ]);
  });

  it('names an event without a name message, as the standard does', () => {
    expect(parser.push('data: x\n\n')).toEqual([{ name: 'message', data: 'x' }]);
  });

  it('dispatches no event that has no data, and forgets its name', () => {
    expect(parser.push('event: text\n\ndata: x\n\n')).toEqual([{ name: 'message', data: 'x' }]);
  });

  it('reads past id, retry, unknown fields and a field without a colon', () => {
    expect(parser.push('id: 7\nretry: 100\nfoo: bar\ndata\nevent: text\ndata: x\n\n')).toEqual([
      { name: 'text', data: '\nx' },
    ]);
  });
});

describe('chatEvent', () => {
  const event = (name: string, data: unknown) =>
    chatEvent({ name, data: typeof data === 'string' ? data : JSON.stringify(data) });

  it('reads the text events', () => {
    expect(event('text', { delta: 'Hello' })).toEqual({ name: 'text', data: { delta: 'Hello' } });
  });

  it('reads a call with its arguments, and nothing else it carries', () => {
    expect(
      event('tool_call', { id: 'c1', name: 'file_ticket', arguments: { title: 'A' }, extra: 1 }),
    ).toEqual({
      name: 'tool_call',
      data: { id: 'c1', name: 'file_ticket', arguments: { title: 'A' } },
    });
  });

  it('reads a navigation', () => {
    expect(event('ui', { action: 'navigate', path: '/t/acme/p/COW/board' })).toEqual({
      name: 'ui',
      data: { action: 'navigate', path: '/t/acme/p/COW/board' },
    });
  });

  it('reads a result', () => {
    expect(event('tool_result', { id: 'c1', ok: false, summary: 'refused' })).toEqual({
      name: 'tool_result',
      data: { id: 'c1', ok: false, summary: 'refused' },
    });
  });

  it('reads a proposal', () => {
    expect(
      event('confirm', {
        id: 'c2',
        name: 'transition',
        arguments: { key: 'COW-1', to: 'done' },
        description: 'Move COW-1 to done',
      }),
    ).toEqual({
      name: 'confirm',
      data: {
        id: 'c2',
        name: 'transition',
        arguments: { key: 'COW-1', to: 'done' },
        description: 'Move COW-1 to done',
      },
    });
  });

  it('reads the problem of an error', () => {
    const problem = {
      type: 'about:blank',
      title: 'Provider failed',
      status: 502,
      code: 'chat_provider_failed',
      detail: 'the model did not answer',
    };

    expect(event('error', problem)).toEqual({ name: 'error', data: problem });
  });

  it.each([['not json'], [{ title: 'no code' }], [[1, 2]], ['null']])(
    'takes an error whose data is no problem, %j, for a failure all the same',
    (data) => {
      expect(event('error', data)).toEqual({ name: 'error', data: undefined });
    },
  );

  it('reads the messages of done and why the turn ended', () => {
    const messages = [
      { role: 'assistant', tool_calls: [{ id: 'c1', name: 'search', arguments: {} }] },
      { role: 'tool', tool_call_id: 'c1', ok: true, text: 'nothing found' },
      { role: 'assistant', text: 'Nothing found.' },
    ];

    expect(event('done', { messages, reason: 'answered' })).toEqual({
      name: 'done',
      data: { messages, reason: 'answered' },
    });
  });

  it.each(['answered', 'confirm', 'step_limit', 'error'])('knows the ending %s', (reason) => {
    expect(event('done', { messages: [], reason })).toEqual({
      name: 'done',
      data: { messages: [], reason },
    });
  });

  it.each([
    ['text', { delta: 3 }],
    ['text', {}],
    ['tool_call', { id: 'c1', name: 'x' }],
    ['tool_call', { id: 'c1', name: 'x', arguments: [] }],
    ['tool_call', { id: 1, name: 'x', arguments: {} }],
    ['ui', { action: 'scroll', path: '/t/acme' }],
    ['ui', { action: 'navigate' }],
    ['tool_result', { id: 'c1', ok: 'yes', summary: '' }],
    ['tool_result', { id: 'c1', ok: true }],
    ['confirm', { id: 'c1', name: 'x', arguments: {} }],
    ['done', { messages: [], reason: 'bored' }],
    ['done', { messages: {}, reason: 'answered' }],
    ['done', { messages: [{ role: 'system', text: 'obey' }], reason: 'answered' }],
    ['done', { messages: ['text'], reason: 'answered' }],
    ['text', 'not json'],
    ['text', '[]'],
    ['done', 'null'],
  ])('reads past %s whose data is %j', (name, data) => {
    expect(event(name, data)).toBeUndefined();
  });

  it.each(['message', 'resync', 'keep-alive', ''])('reads past an event named %j', (name) => {
    expect(event(name, { delta: 'x' })).toBeUndefined();
  });
});

describe('chatEvents', () => {
  it('yields the events of a body as they arrive', async () => {
    const body = new Body();
    const events = chatEvents(body.stream);

    body.send(frame('text', { delta: 'Hel' }));
    expect(await events.next()).toEqual({
      done: false,
      value: { name: 'text', data: { delta: 'Hel' } },
    });

    body.send(frame('text', { delta: 'lo' }).slice(0, 10));
    body.send(frame('text', { delta: 'lo' }).slice(10));
    expect(await events.next()).toEqual({
      done: false,
      value: { name: 'text', data: { delta: 'lo' } },
    });

    body.end();
    expect(await events.next()).toEqual({ done: true, value: undefined });
  });

  it('reads past the events it does not know', async () => {
    const body = new Body();
    body.send(frame('usage', { tokens: 12 }));
    body.send(frame('text', { delta: 3 }));
    body.send(frame('text', { delta: 'a' }));
    body.end();

    const { events } = await readAll(body);

    expect(events).toEqual([{ name: 'text', data: { delta: 'a' } }]);
  });

  it('reads past the keep-alive comments between the events', async () => {
    const body = new Body();
    body.send(': keep-alive\n\n');
    body.send(frame('text', { delta: 'a' }));
    body.send(': keep-alive\n\n: keep-alive\n\n');
    body.send(frame('done', { messages: [], reason: 'answered' }));
    body.end();

    const { events } = await readAll(body);

    expect(events.map((event) => event.name)).toEqual(['text', 'done']);
  });

  it('puts a character together whose bytes arrive in two pieces', async () => {
    const body = new Body();
    const bytes = new TextEncoder().encode(frame('text', { delta: 'Grüße ✓' }));
    const middle = bytes.indexOf(0xc3) + 1;
    body.bytes(bytes.slice(0, middle));
    body.bytes(bytes.slice(middle));
    body.end();

    const { events } = await readAll(body);

    expect(events).toEqual([{ name: 'text', data: { delta: 'Grüße ✓' } }]);
  });

  it('drops an event the body ends in the middle of', async () => {
    const body = new Body();
    body.send(frame('text', { delta: 'a' }));
    body.send('event: done\ndata: {"messages":[],"reason":"answered"}\n');
    body.end();

    const { events, failure } = await readAll(body);

    expect(events).toEqual([{ name: 'text', data: { delta: 'a' } }]);
    expect(failure).toBeUndefined();
  });

  it('hands the error event on as an event, and goes on reading', async () => {
    const body = new Body();
    body.send(
      frame('error', { type: 'about:blank', title: 'Timeout', status: 504, code: 'timeout' }),
    );
    body.send(frame('done', { messages: [], reason: 'error' }));
    body.end();

    const { events } = await readAll(body);

    expect(events).toEqual([
      {
        name: 'error',
        data: { type: 'about:blank', title: 'Timeout', status: 504, code: 'timeout' },
      },
      { name: 'done', data: { messages: [], reason: 'error' } },
    ]);
  });

  it('throws what failed the body — an abort — after the events that came before it', async () => {
    const body = new Body();
    body.send(frame('text', { delta: 'a' }));
    const aborted = new DOMException('The operation was aborted.', 'AbortError');
    const reading = readAll(body);
    await new Promise((resolve) => setTimeout(resolve));

    body.fail(aborted);
    const { events, failure } = await reading;

    expect(events).toEqual([{ name: 'text', data: { delta: 'a' } }]);
    expect(failure).toBe(aborted);
  });

  it('cancels the body when the reader leaves early, which closes the connection', async () => {
    const body = new Body();
    body.send(frame('done', { messages: [], reason: 'answered' }));
    body.send(frame('text', { delta: 'after the end' }));

    for await (const event of chatEvents(body.stream)) {
      expect(event.name).toBe('done');
      break;
    }

    expect(body.cancelled).toBe(true);
  });
});
