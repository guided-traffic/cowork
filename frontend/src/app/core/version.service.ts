import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

/** The body of GET /api/v1/version: the Version schema of backend/api/components/schemas.yaml. */
export interface VersionInfo {
  version: string;
  commit: string;
  build_time: string;
}

@Injectable({ providedIn: 'root' })
export class VersionService {
  private readonly http = inject(HttpClient);

  get(): Observable<VersionInfo> {
    return this.http.get<VersionInfo>('/api/v1/version');
  }
}
