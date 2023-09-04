import axios from "axios";
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { Route, createBrowserRouter, createRoutesFromElements, Navigate, RouterProvider } from "react-router-dom";
import "./App.css";
import { UserContextType, UserProvider } from "./hooks/useUser";
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
