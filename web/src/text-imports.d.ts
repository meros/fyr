// An .html file imported as text: the live view's bridge (../viewbridge/bridge.html), which the Go host embeds too.
// esbuild reads it with its text loader (scripts/build.mjs), the unit tests with test/text-loader.mjs.
declare module "*.html" {
  const text: string;
  export default text;
}
