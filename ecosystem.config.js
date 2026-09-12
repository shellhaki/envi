module.exports = {
  apps: [
    {
      name: "envi-api",
      script: "./bin/envi-api",
      interpreter: "none",
      cwd: __dirname,
      env: {
        ENVI_API_PORT: "8080",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 3000,
      out_file: "./logs/envi-api.out.log",
      error_file: "./logs/envi-api.error.log",
      time: true,
    },
    {
      name: "envi-install",
      script: "./bin/envi-install",
      interpreter: "none",
      cwd: __dirname,
      env: {
        ENVI_SERVER_PORT: "8081",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 3000,
      out_file: "./logs/envi-install.out.log",
      error_file: "./logs/envi-install.error.log",
      time: true,
    },
  ],
};
