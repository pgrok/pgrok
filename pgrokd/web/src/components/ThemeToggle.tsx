import { Button } from "@/components/retroui/Button";
import useTheme, { ThemeChoice } from "../hooks/useTheme";

const LABELS: Record<ThemeChoice, string> = {
  system: "System theme",
  light: "Light theme",
  dark: "Dark theme",
};

// ThemeToggle is a single icon button that cycles System -> Light -> Dark. The
// icon reflects the current choice.
const ICONS: Record<ThemeChoice, () => JSX.Element> = {
  system: MonitorIcon,
  light: SunIcon,
  dark: MoonIcon,
};

export default function ThemeToggle() {
  const { choice, cycle } = useTheme();
  const Icon = ICONS[choice];

  return (
    <Button
      size="icon"
      variant="outline"
      className="size-[31px] p-0 shadow hover:shadow-sm"
      onClick={cycle}
      aria-label={`${LABELS[choice]} (click to change)`}
      title={LABELS[choice]}
    >
      <Icon />
    </Button>
  );
}

const iconProperties = {
  className: "size-4",
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
};

function MonitorIcon() {
  return (
    <svg {...iconProperties}>
      <rect x="2" y="3" width="20" height="14" rx="2" />
      <path d="M8 21h8M12 17v4" />
    </svg>
  );
}

function SunIcon() {
  return (
    <svg {...iconProperties}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg {...iconProperties}>
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </svg>
  );
}
