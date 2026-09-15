// Minimal ESM resolve hook so `node --test` can run source files that use
// NodeNext-style ".js" specifiers pointing at sibling ".ts" files (the repo's
// convention for code that's built later by tsup). Node's own TS type-stripping
// does not remap ".js" -> ".ts", so relative imports fail without this.
export async function resolve(specifier, context, nextResolve) {
  if (specifier.startsWith(".") && specifier.endsWith(".js")) {
    try {
      return await nextResolve(specifier.slice(0, -3) + ".ts", context);
    } catch {
      // fall through to default resolution (and its error) below
    }
  }
  return nextResolve(specifier, context);
}
