import axios from "axios";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { Route, createBrowserRouter, createRoutesFromElements, Navigate, RouterProvider } from "react-router-dom";
import "./App.css";
import { UserContextType, UserProvider } from "./hooks/useUser";
import AdminPage from "./pages/Admin";
import DashboardPage from "./pages/Dashboard";
import ErrorPage from "./pages/Error";
import SignInPage from "./pages/SignIn";
import { FetchIdentityProviderResponse } from "./types";

// Make sure all requests carry over the session cookie
axios.defaults.withCredentials = true;

// Make an initial request to check the authentication state
const user = await axios
  .get("/api/user-info")
  .then((response) => {
    const { displayName, token, url, remoteAddr, isAdmin } = response.data as UserContextType;
    return {
      authed: true,
      displayName: displayName,
      token: token,
      url: url,
      remoteAddr: remoteAddr,
      isAdmin: isAdmin,
    } as UserContextType;
  })
  .catch(() => {
    return {} as UserContextType;
  });

const ProtectedRoute = ({ children }: { children: JSX.Element }): JSX.Element => {
  return user.authed === true ? children : <Navigate to="/sign-in" replace />;
};

export const router = createBrowserRouter(
  createRoutesFromElements(
    <>
      <Route
        path="/sign-in"
        element={<SignInPage />}
        loader={async function ({ request }) {
          // The server redirects here when a blocked user tries to sign in.
          if (new URL(request.url).searchParams.get("error") === "blocked") {
            // Thrown responses are rendered by the route's error element.
            // eslint-disable-next-line @typescript-eslint/only-throw-error
            throw new Response("", {
              status: 403,
              statusText: "Your account has been blocked by an admin. Please contact them to restore access.",
            });
          }

          let data;
          await axios.get("/api/identity-provider").then((response) => {
            data = response.data as FetchIdentityProviderResponse;
          });
          return data;
        }}
        errorElement={<ErrorPage />}
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

      <Route
        path="/admin"
        element={
          <ProtectedRoute>
            <AdminPage />
          </ProtectedRoute>
        }
        errorElement={<ErrorPage />}
      />
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
