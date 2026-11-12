// The server logs failures it handles; tests trigger many on purpose. Set VERBOSE=1 to see them.
if (!process.env.VERBOSE) {
  console.log = console.warn = console.error = () => {};
}
