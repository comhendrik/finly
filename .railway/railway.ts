import { defineRailway, github, preserve, project, service } from "railway/iac";
// This repository manages only its own resources in the environment. Other
// repositories export their own partial name.
// See https://docs.railway.com/infrastructure-as-code#multi-repo-projects
export const partial = "finly";
export default defineRailway(() => {
  const finly = service("finly", {
    source: github("comhendrik/finly", { branch: "master", checkSuites: false }),
    build: { buildEnvironment: "V3", builder: "DOCKERFILE", dockerfilePath: "Dockerfile" },
    healthcheck: "/health",
    healthcheckTimeout: 120,
    replicas: { "europe-west4-drams3a": 1 },
    deploy: { sleepApplication: true },
    env: { DATABASE_URL: preserve() },
  });

  return project("enthusiastic-surprise", {
    variables: { managed: true },
    resources: [finly],
  });
});
