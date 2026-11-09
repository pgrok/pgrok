import { Card } from "@/components/retroui/Card";
import { Tabs } from "@base-ui/react/tabs";
import { Fragment, useState } from "react";
import useUser from "../hooks/useUser";
import CopyButton from "./CopyButton";

const DEFAULT_PORT = "3000";

// GetStarted renders ready-to-run commands for connecting a local server to
// pgrokd, using the signed-in user's token and the server's SSH address.
export default function GetStarted() {
  const user = useUser();
  const [port, setPort] = useState(DEFAULT_PORT);
  const localPort = port || DEFAULT_PORT;

  // remoteAddr is "host:port", with IPv6 hosts in brackets.
  const separator = user.remoteAddr.lastIndexOf(":");
  const sshHost = user.remoteAddr.slice(0, separator).replace(/^\[(.*)]$/, "$1");
  const sshPort = user.remoteAddr.slice(separator + 1);

  return (
    <Card className="mt-6 block w-full">
      <Card.Header className="border-b-2 border-border">
        <Card.Title>Get started</Card.Title>
        <Card.Description className="text-sm">
          Expose a server running on your machine at{" "}
          <a className="underline decoration-primary underline-offset-2 wrap-anywhere" href={user.url}>
            {user.url}
          </a>
          .
        </Card.Description>
      </Card.Header>
      <Card.Content className="space-y-4">
        <label className="flex items-center gap-2 text-sm font-semibold">
          Local port
          <input
            className="w-24 rounded border-2 border-border bg-background px-2 py-1 font-mono font-normal focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            inputMode="numeric"
            maxLength={5}
            placeholder={DEFAULT_PORT}
            value={port}
            onChange={(event) => /^\d*$/.test(event.target.value) && setPort(event.target.value)}
          />
        </label>

        <Tabs.Root defaultValue="pgrok">
          <Tabs.List className="flex gap-2">
            <Tab value="pgrok">pgrok</Tab>
            <Tab value="ssh">Vanilla SSH</Tab>
          </Tabs.List>

          <Tabs.Panel value="pgrok" className="mt-4">
            <ol className="space-y-4">
              <Step n={1} title="Install pgrok">
                <Command value="brew install pgrok" />
                <p className="text-sm text-muted-foreground">
                  Not using Homebrew? Download the archive from the{" "}
                  <a
                    className="underline decoration-primary underline-offset-2"
                    href="https://github.com/pgrok/pgrok/releases"
                    target="_blank"
                    rel="noreferrer"
                  >
                    GitHub Releases
                  </a>{" "}
                  page.
                </p>
              </Step>
              <Step n={2} title="Save your configuration">
                <Command
                  value={`pgrok init --remote-addr ${user.remoteAddr} --forward-addr http://localhost:${localPort} --token ${user.token}`}
                />
              </Step>
              <Step n={3} title="Start the tunnel">
                <Command value="pgrok http" />
              </Step>
            </ol>
          </Tabs.Panel>

          <Tabs.Panel value="ssh" className="mt-4 space-y-2">
            <p className="text-sm">No install needed, any OpenSSH client works:</p>
            <Command value={`ssh -N -R 0:localhost:${localPort} ${sshHost} -p ${sshPort}`} />
            <p className="text-sm text-muted-foreground">When prompted for a password, enter your token.</p>
          </Tabs.Panel>
        </Tabs.Root>
      </Card.Content>
    </Card>
  );
}

function Tab({ value, children }: { value: string; children: React.ReactNode }) {
  return (
    <Tabs.Tab
      value={value}
      className="cursor-pointer rounded border-2 border-border px-3 py-1 font-head text-sm transition-all focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary data-active:bg-primary data-active:text-primary-foreground data-active:shadow"
    >
      {children}
    </Tabs.Tab>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <li className="space-y-2">
      <p className="text-sm font-semibold">
        {n}. {title}
      </p>
      {children}
    </li>
  );
}

function Command({ value }: { value: string }) {
  return (
    <div className="flex items-start gap-2">
      <pre className="min-w-0 flex-1 whitespace-pre-wrap rounded border-2 border-border bg-background px-2 py-1 text-sm wrap-anywhere">
        <code>
          <span className="select-none text-muted-foreground">$ </span>
          {/* Wrap between arguments rather than after hyphens in flags like "-p". */}
          {value.split(" ").map((argument, index) => (
            <Fragment key={index}>
              {index > 0 && " "}
              <span className="inline-block">{argument}</span>
            </Fragment>
          ))}
        </code>
      </pre>
      <CopyButton value={value} label="Copy command" />
    </div>
  );
}
