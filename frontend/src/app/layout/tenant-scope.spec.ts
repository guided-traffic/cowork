import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { MessageService } from 'primeng/api';
import { EventStreamService } from '../core/event-stream.service';
import { MembersService } from '../core/members.service';
import { SessionService } from '../core/session.service';
import { TenantScope } from './tenant-scope';

describe('TenantScope', () => {
  let session: {
    enter: ReturnType<typeof vi.fn<(tenant: string | null) => void>>;
    me: { reload: ReturnType<typeof vi.fn<() => boolean>> };
    oversight: WritableSignal<boolean>;
  };
  let stream: { connect: ReturnType<typeof vi.fn<(tenant: string | null) => void>> };

  beforeEach(() => {
    session = { enter: vi.fn(), me: { reload: vi.fn(() => true) }, oversight: signal(false) };
    stream = { connect: vi.fn() };
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: SessionService,
          // What the offer of the grant reads besides; its own spec tests it.
          useValue: {
            ...session,
            tenant: signal('acme'),
            shown: signal(undefined),
            person: signal(undefined),
            membership: signal(undefined),
          },
        },
        { provide: MembersService, useValue: { setGrant: vi.fn() } },
        { provide: EventStreamService, useValue: stream },
      ],
    });
  });

  async function mount(tenant: string) {
    const fixture = TestBed.createComponent(TenantScope);
    fixture.componentRef.setInput('tenant', tenant);
    await fixture.whenStable();
    return fixture;
  }

  it('makes the slug of the route the tenant of the session and opens the stream of that tenant', async () => {
    await mount('acme');

    expect(session.enter).toHaveBeenCalledExactlyOnceWith('acme');
    expect(stream.connect).toHaveBeenCalledExactlyOnceWith('acme');
  });

  it('follows the slug when the route moves to another tenant', async () => {
    const fixture = await mount('acme');

    fixture.componentRef.setInput('tenant', 'globex');
    await fixture.whenStable();

    expect(session.enter).toHaveBeenLastCalledWith('globex');
    expect(stream.connect).toHaveBeenLastCalledWith('globex');
    expect(session.enter).toHaveBeenCalledTimes(2);
    expect(stream.connect).toHaveBeenCalledTimes(2);
  });

  // docs/adr/0023 D4 as amended 2026-10-10: the plus of the team's group is what `me` says, and
  // no event tells of a change of the setting it follows.
  it('asks for the person again on entering a tenant, and on moving to another', async () => {
    const fixture = await mount('acme');

    expect(session.me.reload).toHaveBeenCalledOnce();

    fixture.componentRef.setInput('tenant', 'globex');
    await fixture.whenStable();

    expect(session.me.reload).toHaveBeenCalledTimes(2);
  });

  it('asks for the person once for a tenant, not again when a grant ends the oversight', async () => {
    session.oversight.set(true);
    const fixture = await mount('acme');

    session.oversight.set(false);
    await fixture.whenStable();

    expect(stream.connect).toHaveBeenLastCalledWith('acme');
    expect(session.me.reload).toHaveBeenCalledOnce();
  });

  it('leaves no tenant and closes the stream when the page is left', async () => {
    const fixture = await mount('acme');

    fixture.destroy();

    expect(session.enter).toHaveBeenLastCalledWith(null);
    expect(stream.connect).toHaveBeenLastCalledWith(null);
  });

  it('offers no grant and opens the stream in a tenant the person works in', async () => {
    const fixture = await mount('acme');

    expect((fixture.nativeElement as HTMLElement).querySelector('app-self-grant')).toBeNull();
    expect(stream.connect).toHaveBeenLastCalledWith('acme');
  });

  describe('a global administrator without a role in the tenant (docs/adr/0034 D2)', () => {
    it('is offered the grant above the page, and the stream of the tenant stays closed', async () => {
      session.oversight.set(true);

      const fixture = await mount('acme');

      const host = fixture.nativeElement as HTMLElement;
      expect(host.querySelector('app-self-grant')).not.toBeNull();
      expect(host.querySelector('.offer app-self-grant')).not.toBeNull();
      expect(host.querySelector('.offer + router-outlet')).not.toBeNull();
      expect(session.enter).toHaveBeenLastCalledWith('acme');
      expect(stream.connect).toHaveBeenCalledExactlyOnceWith(null);
    });

    it('opens the stream, and the offer goes, once the grant made them a member', async () => {
      session.oversight.set(true);
      const fixture = await mount('acme');

      session.oversight.set(false);
      await fixture.whenStable();

      expect(stream.connect).toHaveBeenLastCalledWith('acme');
      expect((fixture.nativeElement as HTMLElement).querySelector('app-self-grant')).toBeNull();
    });
  });

  it('hands the page to the child routes through a router outlet', async () => {
    const fixture = await mount('acme');

    expect((fixture.nativeElement as HTMLElement).querySelector('router-outlet')).not.toBeNull();
  });
});
