import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Attachment } from '../../api/models';
import { FilePreview, previewable } from './file-preview';

function file(contentType: Attachment['content_type'], id = 'a-1'): Attachment {
  return {
    id,
    file_name: 'shot.png',
    content_type: contentType,
    content_url: `/api/v1/teams/acme/projects/COW/tickets/12/attachments/${id}/content`,
  } as Attachment;
}

describe('previewable', () => {
  it.each(['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const)(
    'takes %s, which the backend delivers inline',
    (type) => {
      expect(previewable(file(type))).toBe(true);
    },
  );

  it.each(['image/svg+xml', 'application/pdf', 'text/plain; charset=utf-8'] as const)(
    'refuses %s, which is a download — an SVG is no raster image',
    (type) => {
      expect(previewable(file(type))).toBe(false);
    },
  );
});

describe('FilePreview', () => {
  function render(attachment: Attachment) {
    const fixture = TestBed.createComponent(FilePreview);
    fixture.componentRef.setInput('file', attachment);
    fixture.detectChanges();
    return fixture;
  }

  const image = (fixture: ComponentFixture<FilePreview>) =>
    (fixture.nativeElement as HTMLElement).querySelector('img');

  it('shows a raster image from its own address, lazily, named by the file, and opens it whole', () => {
    const fixture = render(file('image/png'));

    const img = image(fixture) as HTMLImageElement;
    expect(img.getAttribute('src')).toBe(
      '/api/v1/teams/acme/projects/COW/tickets/12/attachments/a-1/content',
    );
    expect(img.getAttribute('loading')).toBe('lazy');
    expect(img.alt).toBe('shot.png');
    const link = img.closest('a') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe(img.getAttribute('src'));
    expect(link.target).toBe('_blank');
    expect(link.rel).toBe('noopener');
  });

  it('shows nothing for a file that is no raster image', () => {
    expect(image(render(file('image/svg+xml')))).toBeNull();
    expect(image(render(file('application/pdf')))).toBeNull();
  });

  it('goes when the image cannot be loaded, and stays gone when the same file comes again', () => {
    const fixture = render(file('image/png'));

    image(fixture)?.dispatchEvent(new Event('error'));
    fixture.detectChanges();
    expect(image(fixture)).toBeNull();

    fixture.componentRef.setInput('file', { ...file('image/png') });
    fixture.detectChanges();
    expect(image(fixture)).toBeNull();

    fixture.componentRef.setInput('file', file('image/png', 'a-2'));
    fixture.detectChanges();
    expect(image(fixture)).not.toBeNull();
  });
});
