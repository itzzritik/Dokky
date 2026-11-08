import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  agentRules: false,
  redirects: async () => [
    { source: "/devices", destination: "/dashboard/devices", permanent: false },
  ],
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
