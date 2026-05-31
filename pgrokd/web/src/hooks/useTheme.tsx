import { useCallback, useEffect, useState } from "react";

export type ThemeChoice = "system" | "light" | "dark";

const STORAGE_KEY = "pgrok-theme";

function readStoredChoice(): ThemeChoice {
  const stored = globalThis.localStorage?.getItem(STORAGE_KEY);
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
}

function systemPrefersDark(): boolean {
  return globalThis.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

// Apply or remove the `.dark` class on <html> for the resolved theme. Kept in
// sync with the inline pre-paint script in index.html so the two never diverge.
function applyResolvedTheme(choice: ThemeChoice) {
  const isDark = choice === "dark" || (choice === "system" && systemPrefersDark());
  globalThis.document?.documentElement.classList.toggle("dark", isDark);
}

/**
 * useTheme manages the System/Light/Dark preference: it persists the choice to
 * localStorage, applies the resolved theme to <html>, and—while in "system"
 * mode—follows the OS color-scheme as it changes.
 */
export default function useTheme() {
  const [choice, setChoice] = useState<ThemeChoice>(readStoredChoice);

  useEffect(() => {
    globalThis.localStorage?.setItem(STORAGE_KEY, choice);
    applyResolvedTheme(choice);

    if (choice !== "system") {
      return;
    }

    // While following the system, re-resolve when the OS preference flips.
    const media = globalThis.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyResolvedTheme("system");
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [choice]);

  // Cycle System -> Light -> Dark -> System.
  const cycle = useCallback(() => {
    const next: Record<ThemeChoice, ThemeChoice> = { system: "light", light: "dark", dark: "system" };
    setChoice((current) => next[current]);
  }, []);

  return { choice, setChoice, cycle };
}
