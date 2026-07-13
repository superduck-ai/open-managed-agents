import { Navigate, Outlet, useLocation } from "@tanstack/react-router";
import { useAuth } from "../../shared/auth/context";
import { normalizeReturnTo } from "../../shared/auth/redirects";

export function ProtectedConsoleLayout() {
  const { status } = useAuth();
  const location = useLocation();

  if (status === "loading") {
    return (
      <div className="grid min-h-screen place-items-center bg-background text-foreground">
        <div className="text-sm text-muted-foreground">Loading Open Managed Agents...</div>
      </div>
    );
  }

  if (status === "anonymous") {
    return <Navigate to="/login" search={{ returnTo: normalizeReturnTo(location.href) }} replace />;
  }

  return <Outlet />;
}
