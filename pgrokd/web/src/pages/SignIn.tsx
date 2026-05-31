import { useLoaderData } from "react-router-dom";
import { buttonVariants } from "../components/ui/Button";
import { Card, CardContent } from "../components/ui/Card";
import { FetchIdentityProviderResponse } from "../types";

export default function SignInPage() {
  const data = useLoaderData() as FetchIdentityProviderResponse;

  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-6 py-12">
      <div className="flex w-full max-w-sm flex-col items-center">
        <img className="h-12 w-auto" src="/pgrok.png" alt="pgrok" />
        <h1 className="mt-6 text-center text-2xl">Sign in to pgrok</h1>

        <Card className="mt-8 w-full">
          <CardContent>
            {data.error ? (
              <p className="text-center text-sm text-muted">{data.error}</p>
            ) : (
              <a className={buttonVariants({ className: "w-full" })} href={data.authURL}>
                Continue with {data.displayName}
              </a>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
