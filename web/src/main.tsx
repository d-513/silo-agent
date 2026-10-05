import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { transport } from "./api";
import App from "./App";
import { ErrorBoundary } from "./ErrorBoundary";
import { queryClient } from "./query";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>
        <ErrorBoundary>
          <App />
        </ErrorBoundary>
      </QueryClientProvider>
    </TransportProvider>
  </StrictMode>,
);
