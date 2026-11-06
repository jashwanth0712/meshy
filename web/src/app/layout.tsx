import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Meshy",
  description: "Decentralized, agent-friendly network access. ENSv2 is the access-control plane; every file an agent touches is audited on Arkiv.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">{children}</body>
    </html>
  );
}
