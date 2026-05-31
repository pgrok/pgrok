import { isRouteErrorResponse, useRouteError } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/Card";

export default function ErrorPage() {
  const error = useRouteError();
  console.error(error);

  return (
    <div className="flex min-h-screen items-center justify-center px-6 py-12">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Oops!</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          <p className="text-sm">Sorry, an unexpected error has occurred.</p>
          <p className="text-sm text-muted">
            <i>{isRouteErrorResponse(error) ? error.statusText : "Unknown error message"}</i>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
