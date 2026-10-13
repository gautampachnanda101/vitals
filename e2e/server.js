const http = require('node:http');
const path = require('node:path');
const { spawn } = require('node:child_process');

const model = {
  name: 'vitals-e2e-stub:latest',
  model: 'vitals-e2e-stub:latest',
  size: 1024,
  size_vram: 1024,
  details: { family: 'test', parameter_size: '1B', quantization_level: 'Q4_0' },
};
const reply = 'Ollama stub response: deterministic dashboard commentary.';
let dashboard;
let stopping = false;

const ollama = http.createServer((req, res) => {
  res.setHeader('Content-Type', 'application/json');
  if (req.method === 'GET' && ['/api/tags', '/api/ps'].includes(req.url)) {
    res.end(JSON.stringify({ models: [model] }));
    return;
  }
  if (req.method !== 'POST' || req.url !== '/api/chat') {
    res.writeHead(404);
    res.end(JSON.stringify({ error: `Unexpected Ollama stub request: ${req.method} ${req.url}` }));
    return;
  }
  let body = '';
  req.setEncoding('utf8');
  req.on('data', chunk => { body += chunk; });
  req.on('error', err => {
    console.error('Ollama stub request failed:', err);
    res.destroy(err);
  });
  req.on('end', () => {
    let input;
    try {
      input = JSON.parse(body);
    } catch (err) {
      res.writeHead(400);
      res.end(JSON.stringify({ error: `Invalid chat JSON: ${err.message}` }));
      return;
    }
    if (input.model !== model.name || input.stream !== false ||
        !Array.isArray(input.messages) || !input.messages.some(m => m.role === 'user')) {
      res.writeHead(400);
      res.end(JSON.stringify({ error: 'Expected the stub model, non-streaming chat and a user message' }));
      return;
    }
    const timer = setTimeout(() => {
      res.end(JSON.stringify({ model: model.name, message: { role: 'assistant', content: reply }, done: true }));
    }, 200);
    res.on('close', () => clearTimeout(timer));
  });
});

function stop(signal) {
  if (stopping) return;
  stopping = true;
  if (dashboard) dashboard.kill(signal);
  ollama.close();
  ollama.closeAllConnections();
}

process.on('SIGINT', () => stop('SIGINT'));
process.on('SIGTERM', () => stop('SIGTERM'));
ollama.on('error', err => {
  console.error('Ollama stub failed:', err);
  stop('SIGTERM');
  process.exitCode = 1;
});
ollama.listen(0, '127.0.0.1', () => {
  const env = { ...process.env };
  for (const key of [
    'OLLAMA_API_KEY', 'OPENAI_API_KEY', 'ANTHROPIC_API_KEY', 'GROQ_API_KEY',
    'MISTRAL_API_KEY', 'TOGETHER_API_KEY', 'OPENROUTER_API_KEY', 'DEEPSEEK_API_KEY',
    'XAI_API_KEY', 'FIREWORKS_API_KEY', 'GEMINI_API_KEY',
  ]) {
    delete env[key];
  }
  const binary = path.resolve(__dirname, '..', process.platform === 'win32' ? 'vitals.exe' : 'vitals');
  dashboard = spawn(binary, [
    'dashboard', '--addr', '127.0.0.1:18173', '--no-open',
    '--ollama-url', `http://127.0.0.1:${ollama.address().port}`,
  ], { env, stdio: 'inherit' });
  dashboard.on('error', err => {
    console.error('Dashboard test server failed:', err);
    stop('SIGTERM');
    process.exitCode = 1;
  });
  dashboard.on('exit', (code, signal) => {
    const wasStopping = stopping;
    stop('SIGTERM');
    process.exitCode = wasStopping ? (process.exitCode || 0) : (code || (signal ? 1 : 0));
  });
});
