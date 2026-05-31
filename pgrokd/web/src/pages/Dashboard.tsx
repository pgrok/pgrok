import { buttonVariants } from "../components/ui/Button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";
import useUser from "../hooks/useUser";

export default function DashboardPage() {
  const user = useUser();

  return (
    <div className="min-h-screen">
      <nav className="border-b-2 border-border bg-card">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-2">
            <img className="h-8 w-auto" src="/pgrok.png" alt="pgrok" />
            <span className="font-head text-lg">pgrok</span>
          </div>
          <a className={buttonVariants({ variant: "outline", size: "sm" })} href="/-/sign-out">
            Sign out
          </a>
        </div>
      </nav>

      <main className="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8">
        <h1 className="text-3xl">Dashboard</h1>

        <Card className="mt-6">
          <CardHeader>
            <CardTitle>User information</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="divide-y-2 divide-border">
              <Row label="Display name">{user.displayName}</Row>
              <Row label="Token">
                <code className="break-all rounded-[--radius-retro] border-2 border-border bg-background px-2 py-1 text-sm">
                  {user.token}
                </code>
              </Row>
              <Row label="Public URL">
                <a
                  className="font-medium underline underline-offset-4 hover:text-primary-hover"
                  target="_blank"
                  rel="noreferrer"
                  href={user.url}
                >
                  {user.url}
                </a>
              </Row>
            </dl>
          </CardContent>
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
