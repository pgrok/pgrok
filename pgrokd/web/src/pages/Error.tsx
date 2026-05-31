import { isRouteErrorResponse, useRouteError } from "react-router-dom";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";

export default function ErrorPage() {
  const error = useRouteError();
  console.error(error);

  return (
    <div className="flex min-h-screen items-center justify-center px-6 py-12">
      <Card className="block w-full max-w-md">
        <Card.Header className="border-b-2 border-border">
          <Card.Title>Oops!</Card.Title>
        </Card.Header>
        <Card.Content className="flex flex-col gap-2">
          <Text>Sorry, an unexpected error has occurred.</Text>
          <Text className="text-muted-foreground">
            <i>{isRouteErrorResponse(error) ? error.statusText : "Unknown error message"}</i>
          </Text>
        </Card.Content>
      </Card>
    </div>
  );
}
