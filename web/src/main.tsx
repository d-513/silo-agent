import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { transport } from "./api";
import App from "./App";
import { Devtools } from "./Devtools";
import { ErrorBoundary } from "./ErrorBoundary";
import { bootLanguage } from "./i18n.ts";
import { queryClient } from "./query";
import "./index.css";

// The language this device last showed is loaded before the first paint, so a
// Polish reader never sees a flash of English.
void bootLanguage().then(() =>
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          <ErrorBoundary>
            <App />
          </ErrorBoundary>
          <Devtools />
        </QueryClientProvider>
      </TransportProvider>
    </StrictMode>,
  ),
);
