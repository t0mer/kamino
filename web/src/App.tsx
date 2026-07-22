import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { Toaster } from "./components/ui/sonner";
import { queryClient } from "./api/queryClient";
import { TokenGate } from "./auth/TokenGate";
import { ApiTokenProvider } from "./hooks/useApi";
import { useToken } from "./auth/useToken";
import { router } from "./router";

function Authed() {
  const { token } = useToken();
  // TokenGate guarantees a token before rendering children.
  return (
    <ApiTokenProvider token={token!}>
      <RouterProvider router={router} />
    </ApiTokenProvider>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <TokenGate>
        <Authed />
      </TokenGate>
      <Toaster />
    </QueryClientProvider>
  );
}
