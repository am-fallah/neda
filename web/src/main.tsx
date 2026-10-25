import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource/fira-sans/latin-400.css";
import "@fontsource/fira-sans/latin-500.css";
import "@fontsource/fira-sans/latin-600.css";
import "@fontsource/fira-sans/latin-700.css";
import "@fontsource/fira-code/latin-400.css";
import "@fontsource/fira-code/latin-500.css";
import "@fontsource/fira-code/latin-600.css";
import "@fontsource-variable/vazirmatn";
import { App } from "./App";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Neda: root element is missing");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
