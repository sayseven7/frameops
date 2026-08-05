import type { NextConfig } from "next";

function loopback(host: string) {
  const octets = host.split(".");
  return host === "localhost" || host === "[::1]" || (octets.length === 4 && octets[0] === "127" && octets.every((octet) => /^\d+$/.test(octet) && Number(octet) <= 255));
}

const configuredAPIURL = process.env.FRAMEOPS_API_URL;
if (!configuredAPIURL) throw new Error("FRAMEOPS_API_URL is required");

let apiURL: URL;
try {
  apiURL = new URL(configuredAPIURL);
} catch {
  throw new Error("FRAMEOPS_API_URL must be a valid HTTP(S) origin");
}
if (!/^https?:$/.test(apiURL.protocol) || apiURL.origin !== configuredAPIURL.replace(/\/$/, "")) {
  throw new Error("FRAMEOPS_API_URL must be a valid HTTP(S) origin");
}
if (apiURL.protocol === "http:" && !loopback(apiURL.hostname)) {
  throw new Error("FRAMEOPS_API_URL must use https outside loopback");
}

const nextConfig: NextConfig = {
  allowedDevOrigins: ["127.0.0.1", "localhost", "192.168.100.31"],
  experimental: { proxyClientMaxBodySize: "34mb" },
  async rewrites() {
    return [{ source: "/v1/:path*", destination: `${apiURL.origin}/v1/:path*` }];
  },
};

export default nextConfig;
