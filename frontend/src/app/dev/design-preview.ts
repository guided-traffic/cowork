import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Slider } from 'primeng/slider';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { SecurityClass, Severity, TicketState, TicketType } from '../api/models';
import { LogoMark, LogoVariant, Wordmark } from '../brand/logo';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../shared/badges';
import { logoColours } from '../theme/cowork-preset';

/**
 * The design preview, for development only (dev.routes.ts): the logo's variants, the palette,
 * the badges and the PrimeNG widgets in cowork's preset — once on a light and once on a dark
 * panel, side by side, because every token is a `light-dark()` pair the panel's
 * `color-scheme` resolves (docs/adr/0052 D3). Where the owner looks at the design and says
 * what stays.
 */
@Component({
  selector: 'app-design-preview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    FormsModule,
    InputText,
    LogoMark,
    SecurityBadge,
    Select,
    SelectButton,
    SeverityBadge,
    Slider,
    StateBadge,
    ToggleSwitch,
    TypeIcon,
    Wordmark,
  ],
  templateUrl: './design-preview.html',
  styleUrl: './design-preview.scss',
})
export class DesignPreview {
  private readonly messages = inject(MessageService);

  protected readonly variants: { id: LogoVariant; name: string; idea: string; chosen?: boolean }[] =
    [
      {
        id: 'board',
        name: 'Board spark',
        idea: 'Three board columns and the spark that works them',
        chosen: true,
      },
      {
        id: 'twin',
        name: 'Twin sparkles',
        idea: 'Two co-workers, a person and an agent: the motif of the reference',
      },
      { id: 'spark-c', name: 'Spark C', idea: 'The c of cowork with the spark in its opening' },
    ];
  protected readonly sizes = [16, 24, 32, 48, 72];
  protected readonly schemes = ['light', 'dark'] as const;
  protected readonly logo = Object.entries(logoColours).map(([name, value]) => ({ name, value }));
  protected readonly steps = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950];
  protected readonly states: TicketState[] = [
    'filed',
    'analysed',
    'decided',
    'in-progress',
    'blocked',
    'done',
    'dropped',
  ];
  protected readonly severities: Severity[] = ['critical', 'high', 'medium', 'low', 'cosmetic'];
  protected readonly securities: SecurityClass[] = ['live', 'boundary', 'hardening'];
  protected readonly types: TicketType[] = ['task', 'bug', 'feature', 'decision', 'question'];
  protected readonly efforts = ['XS', 'S', 'M', 'L'];

  protected readonly progress = signal(50);
  protected readonly effort = signal('M');
  protected readonly watching = signal(true);
  protected readonly chosen = signal<TicketState[]>(['in-progress', 'blocked']);
  protected readonly assignee = signal<string | null>('ada');
  protected readonly people = [
    { id: 'ada', name: 'Ada Lovelace' },
    { id: 'sam', name: 'Sam Rivera' },
  ];

  protected toast(severity: 'success' | 'info' | 'warn' | 'error'): void {
    this.messages.add({
      severity,
      summary: severity === 'error' ? 'The backend cannot be reached' : 'Ticket moved',
      detail:
        severity === 'error' ? 'cowork tries again on its own.' : 'COW-12 is in-progress now.',
      life: 4000,
    });
  }
}
