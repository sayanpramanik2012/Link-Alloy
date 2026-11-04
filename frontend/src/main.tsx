import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { RateUnitProvider } from "./rate-unit";
import "./styles/app.css";

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode><RateUnitProvider><App /></RateUnitProvider></React.StrictMode>,
);
