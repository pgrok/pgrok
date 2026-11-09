import { createContext, useContext, useMemo, useState } from "react";

export interface UserContextType {
  authed: boolean;
  displayName: string;
  token: string;
  url: string;
  remoteAddr: string;
}

interface UserContextValue extends UserContextType {
  /**
   * Replaces the token shown across the app, e.g. after it is regenerated.
   */
  setToken: (token: string) => void;
}

const UserContext = createContext<UserContextValue>({} as UserContextValue);

export function UserProvider({ user, children }: { user: UserContextType; children: JSX.Element }) {
  const [token, setToken] = useState(user.token);
  const value = useMemo(() => ({ ...user, token, setToken }), [user, token]);
  return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}

export default function useUser() {
  return useContext(UserContext);
}
