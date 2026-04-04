export default {
  srcDir: ".",
  srcFiles: [],
  specDir: ".",
  specFiles: [
    "spec/**/*[sS]pec.?(m)js"
  ],
  helpers: [
    "spec/helpers/**/*.?(m)js"
  ],
  esmFilenameExtension: ".mjs",
  enableTopLevelAwait: false,
  env: {
    stopSpecOnExpectationFailure: false,
    stopOnSpecFailure: false,
    random: true
  },
  listenAddress: "localhost",
  hostname: "localhost",
  browser: {
    name: "headlessChrome"
  }
};