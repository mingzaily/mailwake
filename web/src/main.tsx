import { authorizeComponentStyles } from "./lib/csp";
import { createRoot } from "react-dom/client";
import "./styles.css";
import { App } from "./app";
authorizeComponentStyles();
createRoot(document.getElementById("root")!).render(<App />);
