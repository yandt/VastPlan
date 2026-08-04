/** Removes the completed login document so an expired session cannot restore its consumed transaction. */
export function completeAuthenticationNavigation(location: Pick<Location, "replace">, returnTo: string): void {
  location.replace(returnTo);
}

/** A back-forward-cache restore revives React state, but its HttpOnly transaction cookie was already consumed. */
export function installAccessPageResumeGuard(target: Pick<EventTarget, "addEventListener" | "removeEventListener">, reload: () => void): () => void {
  const onPageShow: EventListener = (event) => {
    if ((event as Event & { readonly persisted?: boolean }).persisted === true) reload();
  };
  target.addEventListener("pageshow", onPageShow);
  return () => target.removeEventListener("pageshow", onPageShow);
}

/** Restarts just after the trusted challenge expires, while bounding browser timer input. */
export function authenticationExpiryDelay(expiresAt: string, now = Date.now()): number | undefined {
  const deadline = Date.parse(expiresAt);
  if (!Number.isFinite(deadline)) return undefined;
  return Math.min(Math.max(0, deadline - now + 25), 2_147_483_647);
}
