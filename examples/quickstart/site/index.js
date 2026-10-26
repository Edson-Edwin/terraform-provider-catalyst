const http = require('node:http');

const page = `<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Catalyst Terraform preview</title>
<style>body{font:18px/1.6 system-ui,sans-serif;max-width:680px;margin:12vh auto;padding:24px;background:#f4f7fb;color:#162238}main{background:white;padding:36px;border-radius:16px}h1{line-height:1.2}small{color:#526079}</style>
<main><small>Community Terraform provider for Zoho Catalyst</small>
<h1>Your Terraform deployment is running.</h1>
<p>This AppSail application was deployed using the Catalyst Terraform provider.</p>
<p>Edit this page, run the preview apply command again, and refresh to try an update.</p>
<p>When finished, run the preview destroy command to remove the trial application.</p>
</main></html>`;

http.createServer((_request, response) => {
  response.writeHead(200, {'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store'});
  response.end(page);
}).listen(Number(process.env.X_ZOHO_CATALYST_LISTEN_PORT || 9000), '0.0.0.0');
