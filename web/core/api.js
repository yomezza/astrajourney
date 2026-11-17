import { request } from "./api-client.js";
import { Session } from "./session.js";
import { I18n } from "./i18n.js";

export async function api(path, body, signal) {
  const epoch = Session.epoch;
  try {
    return await request(path, body, signal);
  } catch (error) {
    if (error.status === 401 && epoch === Session.epoch && Session.signedIn)
      window.dispatchEvent(
        new CustomEvent("sessionexpired", { detail: epoch }),
      );
    error.message = I18n.t(error.message);
    throw error;
  }
}
