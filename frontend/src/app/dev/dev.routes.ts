import { Routes } from '@angular/router';

/**
 * Pages for development only: the design preview. The production build replaces this file with
 * dev.routes.prod.ts (angular.json fileReplacements), so none of it reaches the image.
 */
export const devRoutes: Routes = [
  {
    path: 'dev/design',
    loadComponent: () => import('./design-preview').then((m) => m.DesignPreview),
  },
];
