import { StrictMode, useEffect, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, Navigate, RouterProvider, useNavigate } from "react-router";
import { QueryClient, QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { useMe, qk } from "@/api/hooks";
import { UNAUTHORIZED_EVENT } from "@/api/client";
import { Skeleton } from "@/components/ui/primitives";
import { Layout } from "@/pages/Layout";
import { Login } from "@/pages/Login";
import { Dashboard } from "@/pages/Dashboard";
import { Device } from "@/pages/Device";
import { Notifications } from "@/pages/Notifications";
import { GlobalSettingsPage } from "@/features/settings/SettingsPages";
import { VisitsPage } from "@/features/visits/Visits";
import { CostsPage } from "@/features/costs/CostsPage";
import { UsersPage } from "@/features/users/UsersPage";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    // Свежесть данных обеспечивает SSE, поэтому без фонового перезапроса по таймеру.
    queries: { staleTime: 30_000, refetchOnWindowFocus: false, retry: 1 },
  },
});

function RequireAuth({ children }: { children: ReactNode }) {
  const me = useMe();
  const qc = useQueryClient();
  const nav = useNavigate();

  useEffect(() => {
    const onUnauthorized = () => {
      qc.setQueryData(qk.me, null);
      nav("/login", { replace: true });
    };
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [qc, nav]);

  if (me.isPending) return <Skeleton className="m-6 h-40" />;
  if (!me.data) return <Navigate to="/login" replace />;
  return children;
}

const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  {
    element: (
      <RequireAuth>
        <Layout />
      </RequireAuth>
    ),
    children: [
      { path: "/", element: <Dashboard /> },
      { path: "/notifications", element: <Notifications /> },
      { path: "/devices/:id/:tab?", element: <Device /> },
      { path: "/settings", element: <GlobalSettingsPage /> },
      { path: "/visits", element: <VisitsPage /> },
      { path: "/costs", element: <CostsPage /> },
      { path: "/users", element: <UsersPage /> },
      { path: "*", element: <Navigate to="/" replace /> },
    ],
  },
]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster richColors closeButton position="top-right" />
    </QueryClientProvider>
  </StrictMode>,
);
