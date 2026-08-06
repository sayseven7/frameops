import assert from "node:assert/strict";
import test from "node:test";

const apiURL = process.env.FRAMEOPS_API_URL;
const composeInternalAPI = process.env.FRAMEOPS_COMPOSE_INTERNAL_API;

async function nextConfig(url?: string, marker?: string) {
  if (url === undefined) delete process.env.FRAMEOPS_API_URL;
  else process.env.FRAMEOPS_API_URL = url;
  if (marker === undefined) delete process.env.FRAMEOPS_COMPOSE_INTERNAL_API;
  else process.env.FRAMEOPS_COMPOSE_INTERNAL_API = marker;
  return (await import(`./next.config.ts?test=${encodeURIComponent(`${url}:${marker}`)}`)).default;
}

test.after(() => {
  if (apiURL === undefined) delete process.env.FRAMEOPS_API_URL;
  else process.env.FRAMEOPS_API_URL = apiURL;
  if (composeInternalAPI === undefined) delete process.env.FRAMEOPS_COMPOSE_INTERNAL_API;
  else process.env.FRAMEOPS_COMPOSE_INTERNAL_API = composeInternalAPI;
});

test("Next config rejects a missing or invalid FRAMEOPS_API_URL", async () => {
  await assert.rejects(nextConfig(), { message: /FRAMEOPS_API_URL.*required/ });
  await assert.rejects(nextConfig("not a URL"), { message: /FRAMEOPS_API_URL.*valid HTTP/ });
});

test("Next config rejects a remote HTTP FRAMEOPS_API_URL", async () => {
  await assert.rejects(nextConfig("http://api.frameops.example.test"), { message: /FRAMEOPS_API_URL.*https outside loopback/ });
});

test("Next config permits only the marked Compose API HTTP origin", async () => {
  await assert.rejects(nextConfig("http://api:8080"), { message: /FRAMEOPS_API_URL.*https outside loopback/ });
  await assert.rejects(nextConfig("http://api:8081", "1"), { message: /FRAMEOPS_API_URL.*https outside loopback/ });
  const config = await nextConfig("http://api:8080", "1");
  assert.deepEqual(await config.rewrites?.(), [{ source: "/v1/:path*", destination: "http://api:8080/v1/:path*" }]);
});

test("Next config permits HTTP FRAMEOPS_API_URL on loopback", async () => {
  const config = await nextConfig("http://127.0.0.1:8080");
  assert.deepEqual(config.allowedDevOrigins, ["127.0.0.1", "localhost", "192.168.100.31"]);
  assert.equal(config.experimental?.proxyClientMaxBodySize, "34mb");
  assert.deepEqual(await config.rewrites?.(), [{ source: "/v1/:path*", destination: "http://127.0.0.1:8080/v1/:path*" }]);
});

test("Next config rewrites v1 requests to the configured API origin", async () => {
  const config = await nextConfig("https://api.frameops.example.test/");
  assert.deepEqual(await config.rewrites?.(), [{ source: "/v1/:path*", destination: "https://api.frameops.example.test/v1/:path*" }]);
});
