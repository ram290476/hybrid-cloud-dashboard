// Public runtime config from /config.json. Cloud pages require Cognito fields.
// Local kind sets authMode to "dev": same-origin API, no Cognito, and the
// facade injects the HMAC token when Authorization is absent.

export function usesDevAuth(config) {
  return config?.authMode === "dev";
}

export function requiredConfigKeys(config) {
  if (usesDevAuth(config)) return ["apiBaseUrl"];
  return ["apiBaseUrl", "cognitoDomain", "clientId"];
}

export function missingConfigKeys(config) {
  return requiredConfigKeys(config).filter((key) => {
    const value = config?.[key];
    return value == null || value === "" || String(value).includes("your-");
  });
}
