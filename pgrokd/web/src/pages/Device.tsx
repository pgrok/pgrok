import axios, { isAxiosError } from "axios";
import { FormEvent, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import ThemeToggle from "../components/ThemeToggle";
import useUser from "../hooks/useUser";
import { FetchDeviceAuthorizationResponse } from "../types";
import { scopeDescriptions } from "./Consent";

type Step =
  | { name: "enter" }
  | { name: "confirm"; userCode: string; authorization: FetchDeviceAuthorizationResponse }
  | { name: "done"; approved: boolean };

function errorMessage(error: unknown): string {
  if (isAxiosError(error)) {
    return (error.response?.data as { error?: string } | undefined)?.error ?? error.message;
  }
  return String(error);
}

// DevicePage is where users enter the code shown by a device, such as the pgrok
// CLI, and approve its access (RFC 8628). A code from the link the device shows
// is filled in but never approved without the user confirming it.
export default function DevicePage() {
  const user = useUser();
  const [searchParameters] = useSearchParams();
  const [userCode, setUserCode] = useState(searchParameters.get("user_code") ?? "");
  const [step, setStep] = useState<Step>({ name: "enter" });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function lookUp(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const response = await axios.get(`/api/oidc-provider/device-authorizations/${encodeURIComponent(userCode)}`);
      setStep({ name: "confirm", userCode, authorization: response.data as FetchDeviceAuthorizationResponse });
    } catch (error_) {
      setError(errorMessage(error_));
    }
    setSubmitting(false);
  }

  async function decide(code: string, approve: boolean) {
    setSubmitting(true);
    setError("");
    try {
      await axios.post(`/api/oidc-provider/device-authorizations/${encodeURIComponent(code)}`, { approve });
      setStep({ name: "done", approved: approve });
    } catch (error_) {
      setError(errorMessage(error_));
    }
    setSubmitting(false);
  }

  let content: JSX.Element;
  if (user.authed !== true) {
    const returnTo = `${globalThis.location.pathname}${globalThis.location.search}`;
    content = (
      <Card.Content className="flex flex-col gap-4">
        <Text className="text-center">Sign in to connect your device to pgrok.</Text>
        <Button className="w-full" render={<a href={`/-/oidc/auth?returnTo=${encodeURIComponent(returnTo)}`} />}>
          Sign in
        </Button>
      </Card.Content>
    );
  } else if (step.name === "enter") {
    content = (
      <Card.Content>
        <form className="flex flex-col gap-4" onSubmit={(event) => void lookUp(event)}>
          <label className="flex flex-col gap-2">
            <Text className="text-sm font-semibold">Enter the code shown on your device</Text>
            <input
              className="rounded border-2 border-border bg-background px-3 py-2 text-center font-mono text-2xl uppercase tracking-widest focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              value={userCode}
              onChange={(event) => setUserCode(event.target.value)}
              placeholder="XXXX-XXXX"
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              autoFocus
              required
            />
          </label>
          {error && <Text className="text-sm text-destructive">{error}</Text>}
          <Button type="submit" className="w-full" disabled={submitting || userCode.trim() === ""}>
            Continue
          </Button>
        </form>
      </Card.Content>
    );
  } else if (step.name === "confirm") {
    const scopes = step.authorization.scopes.filter((scope) => scope in scopeDescriptions);
    content = (
      <>
        <Card.Header className="border-b-2 border-border">
          <Text as="h4">
            <span className="font-bold">{step.authorization.clientName}</span> wants to access your pgrok account
          </Text>
          <Text className="mt-1 text-sm text-muted-foreground">Signed in as {user.displayName}</Text>
        </Card.Header>
        <Card.Content className="flex flex-col gap-4">
          <Text className="text-sm">
            Only allow this if you started the sign-in yourself and your device shows the code{" "}
            <code className="rounded border-2 border-border bg-background px-1 font-mono">
              {step.userCode.toUpperCase()}
            </code>
            .
          </Text>
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
          {error && <Text className="text-sm text-destructive">{error}</Text>}
          <div className="flex gap-3">
            <Button
              className="flex-1"
              variant="outline"
              disabled={submitting}
              onClick={() => void decide(step.userCode, false)}
            >
              Deny
            </Button>
            <Button className="flex-1" disabled={submitting} onClick={() => void decide(step.userCode, true)}>
              Allow
            </Button>
          </div>
        </Card.Content>
      </>
    );
  } else {
    content = (
      <Card.Content>
        <Text className="text-center">
          {step.approved
            ? "Your device is connected. You can close this tab and return to it."
            : "Access was denied. You can close this tab."}
        </Text>
      </Card.Content>
    );
  }

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center px-6 py-12">
      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>
      <div className="flex w-full max-w-md flex-col items-center">
        <img className="h-12 w-auto" src="/pgrok.png" alt="pgrok" />
        <Text as="h2" className="mt-6 text-center">
          Connect a device
        </Text>
        <Card className="mt-8 block w-full">{content}</Card>
      </div>
    </div>
  );
}
