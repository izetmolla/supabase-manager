import { Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout, ProductLayout, ProjectLayout, RequireAdmin, RequireAuth } from '@/components/layout/layouts'
import { LoginPage, SetupPage } from '@/pages/auth-pages'
import { ProjectsPage } from '@/pages/projects'
import { UsersPage } from '@/pages/users'
import { SystemSettingsRoutes } from '@/pages/system'
import { OverviewPage } from '@/pages/project/overview'
import { ProvidersPage } from '@/pages/project/auth-providers'
import { AuthUrlPage, AuthSettingsPage } from '@/pages/project/auth-settings'
import { SMTPPage } from '@/pages/project/smtp'
import { MigrationsPage, TypesPage } from '@/pages/project/database'
import { FunctionsPage } from '@/pages/project/functions'
import { LogsPage } from '@/pages/project/logs'
import {
  ConfigEditorPage,
  DangerZonePage,
  GeneralSettingsPage,
  NetworkPage,
  PortsPage,
  SecretsPage,
  ServicesPage,
  StoragePage,
} from '@/pages/project/settings'
import { MountsPage } from '@/pages/mounts'
import { ProxyManagerRoutes } from '@/pages/proxy-manager'

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/setup" element={<SetupPage />} />

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/projects" element={<ProjectsPage />} />
        <Route
          path="/settings/users"
          element={
            <RequireAdmin>
              <UsersPage />
            </RequireAdmin>
          }
        />
        <Route
          path="/settings/system/*"
          element={
            <RequireAdmin>
              <SystemSettingsRoutes />
            </RequireAdmin>
          }
        />
        <Route
          path="/settings/storage"
          element={
            <RequireAdmin>
              <MountsPage />
            </RequireAdmin>
          }
        />
        <Route
          path="/proxy-manager/*"
          element={
            <RequireAdmin>
              <ProxyManagerRoutes />
            </RequireAdmin>
          }
        />
      </Route>

      <Route
        path="/projects/:slug"
        element={
          <RequireAuth>
            <ProjectLayout />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route
          path="auth"
          element={
            <ProductLayout
              title="Authentication"
              groups={[
                {
                  title: 'Configuration',
                  items: [
                    { to: 'providers', label: 'Sign In / Providers' },
                    { to: 'url-configuration', label: 'URL Configuration' },
                    { to: 'settings', label: 'Auth Settings' },
                    { to: 'smtp', label: 'Emails (SMTP)' },
                  ],
                },
              ]}
            />
          }
        >
          <Route index element={<Navigate to="providers" replace />} />
          <Route path="providers" element={<ProvidersPage />} />
          <Route path="url-configuration" element={<AuthUrlPage />} />
          <Route path="settings" element={<AuthSettingsPage />} />
          <Route path="smtp" element={<SMTPPage />} />
        </Route>
        <Route
          path="database"
          element={
            <ProductLayout
              title="Database"
              groups={[
                {
                  title: 'Schema',
                  items: [
                    { to: 'migrations', label: 'Migrations' },
                    { to: 'types', label: 'Generated Types' },
                  ],
                },
              ]}
            />
          }
        >
          <Route index element={<Navigate to="migrations" replace />} />
          <Route path="migrations" element={<MigrationsPage />} />
          <Route path="types" element={<TypesPage />} />
        </Route>
        <Route path="functions" element={<FunctionsPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route
          path="settings"
          element={
            <ProductLayout
              title="Project Settings"
              groups={[
                {
                  title: 'Project',
                  items: [
                    { to: 'general', label: 'General' },
                    { to: 'services', label: 'Services' },
                    { to: 'ports', label: 'Ports' },
                    { to: 'network', label: 'Network' },
                    { to: 'storage', label: 'Storage' },
                    { to: 'secrets', label: 'Secrets' },
                  ],
                },
                {
                  title: 'Advanced',
                  items: [
                    { to: 'config', label: 'config.toml' },
                    { to: 'danger', label: 'Danger zone' },
                  ],
                },
              ]}
            />
          }
        >
          <Route index element={<Navigate to="general" replace />} />
          <Route path="general" element={<GeneralSettingsPage />} />
          <Route path="services" element={<ServicesPage />} />
          <Route path="ports" element={<PortsPage />} />
          <Route path="network" element={<NetworkPage />} />
          <Route path="storage" element={<StoragePage />} />
          <Route path="secrets" element={<SecretsPage />} />
          <Route path="config" element={<ConfigEditorPage />} />
          <Route path="danger" element={<DangerZonePage />} />
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/projects" replace />} />
    </Routes>
  )
}
