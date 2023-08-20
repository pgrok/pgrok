import { useLoaderData } from "react-router-dom";
import { FetchIdentityProviderResponse } from "../types";

export default function SignInPage() {
  const data = useLoaderData() as FetchIdentityProviderResponse;
  if (data.error) {
    return <p>{data.error}</p>;
  }
  return (
    <>
      <div className="flex min-h-full flex-1 flex-col justify-center px-6 py-12 lg:px-8">
        <div className="sm:mx-auto sm:w-full sm:max-w-sm">
          <img className="mx-auto h-10 w-auto" src="/pgrok.svg" />
          <h2 className="mt-10 text-center text-2xl font-bold leading-9 tracking-tight text-gray-900">
            Sign in to your account
          </h2>
        </div>

        <div className="mt-10 sm:mx-auto sm:w-full sm:max-w-sm">
          <div>
            <a
              className="flex w-full justify-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold leading-6 text-white shadow-sm hover:bg-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
              href={`${data.authURL}`}
            >
              Continue with {data.displayName}
            </a>
          </div>
        </div>
      </div>
    </>
  );
}
