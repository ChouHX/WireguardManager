import { createRoot } from "react-dom/client";
import { MantineProvider, createTheme } from "@mantine/core";
import "@mantine/core/styles.css";
import App from "./App";
import "./style.css";
const theme = createTheme({
  primaryColor: "teal",
  fontFamily: "Inter, Segoe UI, Microsoft YaHei, sans-serif",
  defaultRadius: "md",
  headings: { fontFamily: "Inter, Segoe UI, Microsoft YaHei, sans-serif" },
});
createRoot(document.getElementById("root")!).render(
  <MantineProvider theme={theme} defaultColorScheme="light">
    <App />
  </MantineProvider>,
);
