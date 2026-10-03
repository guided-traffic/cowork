import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EventStreamService } from '../core/event-stream.service';
import { SessionService } from '../core/session.service';
import { TenantScope } from './tenant-scope';

describe('TenantScope', () => {
  let session: { enter: ReturnType<typeof vi.fn<(tenant: string | null) => void>> };
  let stream: { connect: ReturnType<typeof vi.fn<(tenant: string | null) => void>> };

  beforeEach(() => {
    session = { enter: vi.fn() };
    stream = { connect: vi.fn() };
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        { provide: SessionService, useValue: session },
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

  it('leaves no tenant and closes the stream when the page is left', async () => {
    const fixture = await mount('acme');

    fixture.destroy();

    expect(session.enter).toHaveBeenLastCalledWith(null);
    expect(stream.connect).toHaveBeenLastCalledWith(null);
  });

  it('hands the page to the child routes through a router outlet', async () => {
    const fixture = await mount('acme');

    expect((fixture.nativeElement as HTMLElement).querySelector('router-outlet')).not.toBeNull();
  });
});
