// An administrator can hand out a password that its owner has to replace before
// the account works again. The API enforces that on every request; this module
// only carries the answer around the console so it can show the form instead of
// a wall of permission errors.

const STORAGE_KEY = 'must_change_password';
const EVENT = 'must-change-password-changed';

export const PASSWORD_CHANGE_REQUIRED = 'PASSWORD_CHANGE_REQUIRED';

export function isPasswordChangePending(): boolean {
  if (typeof localStorage === 'undefined') return false;
  return localStorage.getItem(STORAGE_KEY) === 'true';
}

export function setPasswordChangePending(pending: boolean): void {
  if (typeof localStorage === 'undefined') return;
  if (pending) {
    localStorage.setItem(STORAGE_KEY, 'true');
  } else {
    localStorage.removeItem(STORAGE_KEY);
  }
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(EVENT));
  }
}

export function onPasswordChangePendingChange(listener: () => void): () => void {
  if (typeof window === 'undefined') return () => {};
  window.addEventListener(EVENT, listener);
  return () => window.removeEventListener(EVENT, listener);
}
