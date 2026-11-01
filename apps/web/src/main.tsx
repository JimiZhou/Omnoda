import React from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./ui";
import "./styles.css";

const saved = localStorage.getItem("omnoda_theme");
if (saved) document.documentElement.dataset.theme = saved;

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>
);
