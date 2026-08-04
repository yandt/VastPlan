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
