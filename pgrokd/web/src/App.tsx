import axios, { isAxiosError } from "axios";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { Route, createBrowserRouter, createRoutesFromElements, Navigate, RouterProvider } from "react-router-dom";
import "./App.css";
import { UserContextType, UserProvider } from "./hooks/useUser";
import ConsentPage from "./pages/Consent";
import DashboardPage from "./pages/Dashboard";
import DevicePage from "./pages/Device";
import ErrorPage from "./pages/Error";
import SignInPage from "./pages/SignIn";
import { FetchAuthRequestResponse, FetchIdentityProviderResponse } from "./types";

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
        errorElement={<ErrorPage />}
      />

      <Route
        path="/consent"
        element={
          <ProtectedRoute>
            <ConsentPage />
          </ProtectedRoute>
        }
        loader={async function ({ request }) {
          const id = new URL(request.url).searchParams.get("authRequestID") ?? "";
          return axios
            .get(`/api/oidc-provider/auth-requests/${encodeURIComponent(id)}`)
            .then((response) => response.data as FetchAuthRequestResponse)
            .catch((error: unknown) => {
              const message = isAxiosError(error)
                ? (error.response?.data as { error?: string } | undefined)?.error
                : undefined;
              return { error: message ?? "The sign-in request is invalid or has expired." } as FetchAuthRequestResponse;
            });
        }}
        errorElement={<ErrorPage />}
      />

      <Route path="/device" element={<DevicePage />} errorElement={<ErrorPage />} />

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
