import { Session } from "../../core/session.js";
import { w } from "../selectors.js";
export function can(permission) {
  if (w().company_owner === Session.data.state.user.id) return true;
  const member = w().members?.find((m) => m.id === Session.data.state.user.id),
    role = w().roles?.find((r) => r.id === member?.company_role);
  return role?.permissions.includes(permission) || false;
}
