import { createBrowserRouter, Navigate } from "react-router-dom";
import { Connect } from "./screens/Connect";
import { Setup } from "./screens/Setup";
import { Run } from "./screens/Run";
import { History } from "./screens/History";
import { AppLayout } from "./components/Nav";

export const router = createBrowserRouter([
  {
    element: <AppLayout />,
    children: [
      { path: "/", element: <Navigate to="/setup" replace /> },
      { path: "/connect", element: <Connect /> },
      { path: "/setup", element: <Setup /> },
      { path: "/run/:id", element: <Run /> },
      { path: "/history", element: <History /> },
      { path: "*", element: <Navigate to="/setup" replace /> },
    ],
  },
]);
