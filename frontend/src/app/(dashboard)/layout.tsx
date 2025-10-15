import AdminPanelLayout from "@/components/admin-panel/admin-panel-layout";
import { AuthGuard } from "@/components/auth/auth-guard";

export default function DemoLayout({
  children
}: {
  children: React.ReactNode;
}) {
  return (
    <AuthGuard requireAuth={true}>
      <AdminPanelLayout>{children}</AdminPanelLayout>
    </AuthGuard>
  );
}
