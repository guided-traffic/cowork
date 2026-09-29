import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

/** The body of GET /api/v1/version; see internal/httpserver/server.go. */
export interface VersionInfo {
  version: string;
  commit: string;
  buildTime: string;
}

@Injectable({ providedIn: 'root' })
export class VersionService {
  private readonly http = inject(HttpClient);

  get(): Observable<VersionInfo> {
    return this.http.get<VersionInfo>('/api/v1/version');
  }
}
