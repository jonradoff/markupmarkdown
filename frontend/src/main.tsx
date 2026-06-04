import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import "./styles.css";
import { applyTheme, getTheme } from "./utils/theme";
import { AuthProvider } from "./auth";
import { DialogProvider } from "./components/Dialogs";
import { ToastProvider } from "./components/Toast";

applyTheme(getTheme());

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <DialogProvider>
        <ToastProvider>
          <AuthProvider>
            <App />
          </AuthProvider>
        </ToastProvider>
      </DialogProvider>
    </BrowserRouter>
  </React.StrictMode>
);
