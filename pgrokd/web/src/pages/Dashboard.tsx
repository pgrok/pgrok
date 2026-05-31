import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
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
          <Button size="sm" variant="outline" render={<a href="/-/sign-out" />}>
            Sign out
          </Button>
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
                <code className="break-all rounded border-2 border-border bg-background px-2 py-1 text-sm">
                  {user.token}
                </code>
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
