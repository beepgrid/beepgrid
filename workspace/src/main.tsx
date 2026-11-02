import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { client } from "./data";
import App from "./App";
import "./typography-presets.css";
import "./styles.css";
import "./typography-picker.css";
import { TypographyPicker } from "./TypographyPicker";
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <App />
      {["localhost", "127.0.0.1", "[::1]"].includes(location.hostname) ? (
        <TypographyPicker />
      ) : null}
    </QueryClientProvider>
  </StrictMode>,
);
