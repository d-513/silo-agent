import { Navigate, Route, Routes } from "react-router-dom";
import { AdminConnectors } from "./AdminConnectors";
import { AdminDrives } from "./AdminDrives";
import { AdminDebug, AdminLayout, AdminSearchExtract, AdminSettings } from "./Admin";
import { useAuth } from "./auth";
import { AdminSkills } from "./Skills";

function AdminGate() {
  const { admin } = useAuth();
  if (!admin) return <Navigate to="/" replace />;
  return <AdminLayout />;
}

// Everything under /admin, loaded as one chunk the first time an admin opens it.
export default function AdminApp() {
  return (
    <Routes>
      <Route element={<AdminGate />}>
        <Route index element={<Navigate to="settings" replace />} />
        <Route path="settings" element={<AdminSettings />} />
        <Route path="connectors/*" element={<AdminConnectors />} />
        <Route path="skills" element={<AdminSkills />} />
        <Route path="search-extract" element={<AdminSearchExtract />} />
        <Route path="drives" element={<AdminDrives />} />
        <Route path="debug" element={<AdminDebug />} />
      </Route>
      <Route path="*" element={<Navigate to="/" />} />
    </Routes>
  );
}
