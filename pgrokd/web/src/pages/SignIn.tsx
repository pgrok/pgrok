import { useLoaderData } from "react-router-dom";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import { FetchIdentityProviderResponse } from "../types";

export default function SignInPage() {
  const data = useLoaderData() as FetchIdentityProviderResponse;

  return (
    <div className="flex min-h-screen flex-col items-center justify-center px-6 py-12">
      <div className="flex w-full max-w-sm flex-col items-center">
        <img className="h-12 w-auto" src="/pgrok.png" alt="pgrok" />
        <Text as="h2" className="mt-6 text-center">
          Sign in to pgrok
        </Text>

        <Card className="mt-8 block w-full">
          <Card.Content>
            {data.error ? (
              <Text className="text-center text-muted-foreground">{data.error}</Text>
            ) : (
              <Button className="w-full" render={<a href={data.authURL} />}>
                Continue with {data.displayName}
              </Button>
            )}
          </Card.Content>
        </Card>
      </div>
    </div>
  );
}
