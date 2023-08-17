import { createContext, useContext } from "react";

export interface UserContextType {
  authed: boolean;
  displayName: string;
  token: string;
  url: string;
}

const UserContext = createContext<UserContextType>({} as UserContextType);

export function UserProvider({ user, children }: { user: UserContextType; children: JSX.Element }) {
  return <UserContext.Provider value={user}>{children}</UserContext.Provider>;
}

export default function useUser() {
  return useContext(UserContext);
}
