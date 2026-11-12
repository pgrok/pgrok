import axios, { isAxiosError } from "axios";
import { useState } from "react";
import { useLoaderData } from "react-router-dom";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import ThemeToggle from "../components/ThemeToggle";
import useUser from "../hooks/useUser";
import { FetchAuthRequestResponse } from "../types";

// Plain-language descriptions of the scopes a client can request.
export const scopeDescriptions: Record<string, string> = {
  openid: "Know who you are on pgrok",
  profile: "See your name and identifier",
  email: "See your email address",
  offline_access: "Stay signed in until you revoke access",
};

export default function ConsentPage() {
  const data = useLoaderData() as FetchAuthRequestResponse;
  const user = useUser();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(data.error ?? "");

  async function decide(approve: boolean) {
    setSubmitting(true);
    try {
      const response = await axios.post(`/api/oidc-provider/auth-requests/${encodeURIComponent(data.id)}`, {
        approve,
      });
      const { redirectURL, formPost } = response.data as {
        redirectURL: string;
        formPost?: Record<string, string[]>;
      };
      if (!formPost) {
        globalThis.location.assign(redirectURL);
        return;
      }
      // The client asked for response_mode=form_post: POST the response.
      const form = document.createElement("form");
      form.method = "POST";
      form.action = redirectURL;
      for (const [name, values] of Object.entries(formPost)) {
        for (const value of values) {
          const input = document.createElement("input");
          input.type = "hidden";
          input.name = name;
          input.value = value;
          form.append(input);
        }
      }
      document.body.append(form);
      form.submit();
    } catch (error_) {
      setSubmitting(false);
      setError(
        isAxiosError(error_)
          ? ((error_.response?.data as { error?: string } | undefined)?.error ?? error_.message)
          : String(error_),
      );
    }
  }

  const redirectHost = data.redirectURI ? new URL(data.redirectURI).host : "";
  const scopes = (data.scopes ?? []).filter((scope) => scope in scopeDescriptions);

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center px-6 py-12">
      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>
      <div className="flex w-full max-w-md flex-col items-center">
        <img className="h-12 w-auto" src="/pgrok.png" alt="pgrok" />

        <Card className="mt-8 block w-full">
          {error ? (
            <Card.Content>
              <Text className="text-center text-muted-foreground">{error}</Text>
            </Card.Content>
          ) : (
            <>
              <Card.Header className="border-b-2 border-border">
                <Text as="h4">
                  <span className="font-bold">{data.clientName}</span> wants to access your pgrok account
                </Text>
                <Text className="mt-1 text-sm text-muted-foreground">Signed in as {user.displayName}</Text>
              </Card.Header>
              <Card.Content className="flex flex-col gap-4">
                {scopes.length > 0 && (
                  <div>
                    <Text className="text-sm font-semibold">This will allow it to:</Text>
                    <ul className="mt-2 list-disc space-y-1 pl-5">
                      {scopes.map((scope) => (
                        <Text as="li" key={scope} className="text-sm">
                          {scopeDescriptions[scope]}
                        </Text>
                      ))}
                    </ul>
                  </div>
                )}
                <Text className="text-sm text-muted-foreground">
                  After you allow access, you will be sent to <span className="font-semibold">{redirectHost}</span>.
                </Text>
                <div className="flex gap-3">
                  <Button
                    className="flex-1"
                    variant="outline"
                    disabled={submitting}
                    onClick={() => void decide(false)}
                  >
                    Deny
                  </Button>
                  <Button className="flex-1" disabled={submitting} onClick={() => void decide(true)}>
                    Allow
                  </Button>
                </div>
              </Card.Content>
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
