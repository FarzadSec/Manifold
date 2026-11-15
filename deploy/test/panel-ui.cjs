// What an admin sees: creates an OpenVPN, a WireGuard and an L2TP inbound and a
// client through the panel's own forms in a real browser, then requires the
// client's windows and its subscription page to show something to hand out for
// each. The API alone passing says nothing about this.
//
//   PANEL_URL=http://host:port/path PANEL_USER=… PANEL_PASSWORD=… node deploy/test/panel-ui.cjs
//
// Needs frontend/node_modules (npm ci) with a Chromium for Playwright, and the
// three engines' programs on the panel's server. Run from the repository root.
const { createRequire } = require('module');
const path = require('path');

const { chromium } = createRequire(path.join(__dirname, '../../frontend/'))('playwright');

const BASE = (process.env.PANEL_URL || '').replace(/\/$/, '');
const USER = process.env.PANEL_USER;
const PASSWORD = process.env.PANEL_PASSWORD;
const CLIENT = 'ui-client';
const INBOUNDS = [
  { remark: 'ui-ovpn', protocol: 'openvpn', port: '21390' },
  { remark: 'ui-wg', protocol: 'wireguard', port: '51990' },
  // The form fixes the port of an L2TP inbound itself.
  { remark: 'ui-l2tp', protocol: 'l2tp' },
];
const HANDED_OUT = ['OpenVPN profile', 'WireGuard config', 'L2TP account', `Username: ${CLIENT}`];

function fail(message) {
  console.error(`FAIL: ${message}`);
  process.exit(1);
}

async function saved(page, route, action) {
  const [response] = await Promise.all([
    page.waitForResponse((r) => r.url().includes(route) && r.request().method() === 'POST'),
    action(),
  ]);
  const body = await response.text();
  if (!body.includes('"success":true')) fail(`${route} was refused: ${body.slice(0, 300)}`);
}

async function shows(what, locator, texts) {
  const text = (await locator.innerText()).replace(/\s+/g, ' ');
  for (const wanted of texts) {
    if (!text.includes(wanted)) fail(`${what} does not show "${wanted}". It shows: ${text.slice(0, 1500)}`);
  }
  console.log(`== ${what} shows ${texts.length} things to hand out`);
}

(async () => {
  if (!BASE || !USER || !PASSWORD) fail('set PANEL_URL, PANEL_USER and PANEL_PASSWORD');
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1400, height: 1600 } });
  page.on('pageerror', (error) => fail(`the page threw: ${error.message}`));
  const dialog = () => page.locator('.ant-modal-container').last();
  const field = (label) => dialog().locator(`.ant-form-item:has(label:text-is("${label}"))`);

  await page.goto(`${BASE}/`);
  await page.fill('input[type="text"]', USER);
  await page.fill('input[type="password"]', PASSWORD);
  await page.keyboard.press('Enter');
  await page.waitForURL(/\/panel/);

  await page.goto(`${BASE}/panel/inbounds`);
  for (const inbound of INBOUNDS) {
    await page.click('button:has-text("Add Inbound")');
    await field('Remark').locator('input').fill(inbound.remark);
    await field('Protocol').locator('select').selectOption(inbound.protocol);
    if (inbound.port) await field('Port').locator('input').fill(inbound.port);
    await saved(page, '/panel/api/inbounds/add', () => dialog().locator('button:has-text("Create")').click());
    await page.waitForSelector(`tr:has-text("${inbound.remark}")`);
    console.log(`== the form created the ${inbound.protocol} inbound`);
  }

  await page.goto(`${BASE}/panel/clients`);
  await page.click('button:has-text("Add Clients")');
  await field('Email').locator('input').fill(CLIENT);
  await dialog().locator('button:has-text("Select all")').click();
  await saved(page, '/panel/api/clients/add', () => dialog().locator('button:has-text("Create")').click());
  const row = page.locator(`tr:has-text("${CLIENT}")`).first();
  await row.waitFor();
  console.log('== the form created the client on every inbound');

  await row.locator('button[aria-label="Client Information"]').click();
  await dialog().locator('text=L2TP account').first().waitFor({ timeout: 15000 }).catch(() => {});
  await shows('the information window', dialog(), HANDED_OUT);
  const subscription = await dialog().locator('a[href*="://"]').first().getAttribute('href');
  await page.keyboard.press('Escape');

  await row.locator('button[aria-label="QR Code"]').click();
  await dialog().locator('text=L2TP account').first().waitFor({ timeout: 15000 }).catch(() => {});
  await shows('the QR window', dialog(), ['OpenVPN profile', 'WireGuard config', 'L2TP account']);
  await page.keyboard.press('Escape');

  if (!subscription) fail('the information window has no subscription address');
  // The address is built for whoever reads the window; this browser reaches the same server through the panel's host.
  const address = new URL(subscription);
  address.hostname = new URL(BASE).hostname;
  const customer = await browser.newPage({ viewport: { width: 1200, height: 1600 } });
  const answer = await customer.goto(address.toString());
  if (answer.status() !== 200) fail(`the subscription page answered ${answer.status()} at ${address}`);
  await customer.click('[role=tab]:has-text("Configs")');
  await shows('the subscription page', customer.locator('body'), ['OpenVPN profile', 'L2TP account', `Username: ${CLIENT}`]);

  await browser.close();
  console.log('PASS');
})().catch((error) => fail(error.message.slice(0, 800)));
