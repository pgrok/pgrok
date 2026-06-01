import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import { useState } from "react";
import ThemeToggle from "../components/ThemeToggle";
import useUser from "../hooks/useUser";

export default function DashboardPage() {
  const user = useUser();

  return (
    <div className="min-h-screen">
      <nav className="border-b-2 border-border bg-card">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-2">
            <img className="h-8 w-auto" src="/pgrok.png" alt="pgrok" />
            <Text as="h4">pgrok</Text>
          </div>
          <div className="flex items-center gap-2">
            <ThemeToggle />
            <Button size="sm" variant="outline" render={<a href="/-/sign-out" />}>
              Sign out
            </Button>
          </div>
        </div>
      </nav>

      <main className="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8">
        <Text as="h1">Dashboard</Text>

        <Card className="mt-6 block w-full">
          <Card.Header className="border-b-2 border-border">
            <Card.Title>User information</Card.Title>
          </Card.Header>
          <Card.Content>
            <dl className="divide-y-2 divide-border">
              <Row label="Display name">{user.displayName}</Row>
              <Row label="Token">
                <div className="flex items-center gap-2">
                  <code className="break-all rounded border-2 border-border bg-background px-2 py-1 text-sm">
                    {user.token}
                  </code>
                  <CopyButton value={user.token} />
                </div>
              </Row>
              <Row label="Public URL">
                <a
                  className="font-sans underline decoration-primary underline-offset-2 hover:underline"
                  href={user.url}
                  target="_blank"
                  rel="noreferrer"
                >
                  {user.url}
                </a>
              </Row>
            </dl>
          </Card.Content>
        </Card>
      </main>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1 py-4 sm:grid-cols-3 sm:gap-4">
      <dt className="text-sm font-semibold">{label}</dt>
      <dd className="text-sm sm:col-span-2">{children}</dd>
    </div>
  );
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied (e.g. insecure context); ignore.
    }
  }

  return (
    <Button
      size="icon"
      variant="outline"
      className="size-[31px] p-0 shadow hover:shadow-sm"
      onClick={() => void copy()}
      aria-label={copied ? "Copied" : "Copy token"}
      title={copied ? "Copied" : "Copy token"}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}

function CopyIcon() {
  return (
    <svg
      className="size-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      className="size-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
}
