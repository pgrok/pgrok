import axios from "axios";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import {
  Route,
  useLoaderData,
  createBrowserRouter,
  createRoutesFromElements,
  Navigate,
  RouterProvider,
} from "react-router-dom";
import ErrorPage from "./ErrorPage";
import useUser, { UserContextType, UserProvider } from "./hooks/useUser";

// Make sure all requests carry over the session cookie
axios.defaults.withCredentials = true;

// Make an initial request to check the authentication state
const user = await axios
  .get("/api/user-info")
  .then((response) => {
    const { displayName, token, url } = response.data as UserContextType;
    return {
      authed: true,
      displayName: displayName,
      token: token,
      url: url,
    } as UserContextType;
  })
  .catch(() => {
    return {} as UserContextType;
  });

interface FetchIdentityProviderResponse {
  error: string;
  displayName: string;
  authURL: string;
}

const SignInPage = () => {
  const data = useLoaderData() as FetchIdentityProviderResponse;
  if (data.error) {
    return <p>{data.error}</p>;
  }
  return (
    <p>
      Please sign in with <a href={`${data.authURL}`}>{data.displayName}</a>.
    </p>
  );
};

const DashboardPage = () => {
  const user = useUser();
  return (
    <p>
      Welcome, {user.displayName}. Your token is <code>{user.token}</code>, and the URL is{" "}
      <a target="_blank" rel="noreferrer" href={user.url}>
        {user.url}
      </a>
      .
    </p>
  );
};

const ProtectedRoute = ({ children }: { children: JSX.Element }): JSX.Element => {
  return user.authed === true ? children : <Navigate to="/sign-in" replace />;
};

export const router = createBrowserRouter(
  createRoutesFromElements(
    <>
      <Route
        path="/sign-in"
        element={<SignInPage />}
        loader={async function () {
          let data;
          await axios.get("/api/identity-provider").then((response) => {
            data = response.data as FetchIdentityProviderResponse;
          });
          return data;
        }}
      />

      <Route
        path="/"
        element={
          <ProtectedRoute>
            <DashboardPage />
          </ProtectedRoute>
        }
        errorElement={<ErrorPage />}
      ></Route>
    </>,
  ),
);

ReactDOM.createRoot(document.querySelector("#root")!).render(
  <StrictMode>
    <UserProvider user={user}>
      <RouterProvider router={router} />
    </UserProvider>
  </StrictMode>,
);
