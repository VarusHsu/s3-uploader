const dropZone = document.getElementById('dropZone');
const fileInput = document.getElementById('fileInput');
const pickBtn = document.getElementById('pickBtn');
const statusEl = document.getElementById('status');
const progressBarEl = document.getElementById('progressBar');
const progressTextEl = document.getElementById('progressText');
const bucketEl = document.getElementById('bucket');
const prefixEl = document.getElementById('prefix');
const apiBaseEl = document.getElementById('apiBase');

function setStatus(message, asError = false) {
  statusEl.textContent = message;
  statusEl.classList.toggle('error', asError);
}

function setProgress(percent, extraText = '') {
  const value = Math.max(0, Math.min(100, Math.round(percent)));
  progressBarEl.style.width = `${value}%`;
  progressTextEl.textContent = extraText ? `${value}% · ${extraText}` : `${value}%`;
}

function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function formatRate(bytesPerSecond) {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return '0 B/s';
  return `${formatSize(Math.round(bytesPerSecond))}/s`;
}

function normalizeApiBase() {
  const raw = apiBaseEl.value.trim();
  return raw.replace(/\/$/, '');
}

function buildKey(prefix, filename) {
  const safeName = filename.split('/').pop();
  if (!prefix) return safeName;
  return `${prefix.replace(/\/$/, '')}/${safeName}`;
}

async function requestUploadUrl(file) {
  const bucket = bucketEl.value.trim();
  if (!bucket) {
    throw new Error('请先填写 Bucket');
  }

  const apiBase = normalizeApiBase();
  const key = buildKey(prefixEl.value.trim(), file.name);

  const formData = new FormData();
  formData.append('bucket', bucket);
  formData.append('key', key);
  formData.append('filename', file.name);
  formData.append('contentType', file.type || 'application/octet-stream');

  const resp = await fetch(`${apiBase}/upload-url`, {
    method: 'POST',
    body: formData,
    mode: 'cors'
  });

  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(`获取上传地址失败(${resp.status}): ${text}`);
  }

  return resp.json();
}

async function uploadToS3(file, signed, onProgress) {
  const headers = {};
  const signedHeaders = signed.headers || {};

  // Browsers do not allow setting some reserved headers (for example `host`).
  Object.entries(signedHeaders).forEach(([k, v]) => {
    if (!/^host$/i.test(k)) {
      headers[k] = v;
    }
  });

  if (!Object.keys(headers).some((k) => /^content-type$/i.test(k))) {
    headers['Content-Type'] = file.type || 'application/octet-stream';
  }

  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(signed.method || 'PUT', signed.uploadUrl, true);

    Object.entries(headers).forEach(([k, v]) => {
      xhr.setRequestHeader(k, v);
    });

    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable) return;
      const percent = event.total > 0 ? (event.loaded / event.total) * 100 : 0;
      if (onProgress) {
        onProgress(percent, event.loaded, event.total, performance.now());
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr);
        return;
      }
      const body = (xhr.responseText || '').slice(0, 500);
      reject(new Error(`上传到 S3 失败(${xhr.status}): ${body}`));
    };

    xhr.onerror = () => {
      reject(new Error('上传到 S3 失败：网络错误或跨域拦截'));
    };

    xhr.onabort = () => {
      reject(new Error('上传已取消'));
    };

    xhr.send(file);
  });
}

async function uploadFile(file) {
  let lastLoaded = 0;
  let lastTickMs = 0;
  let smoothedRate = 0;

  setProgress(0, '0 B/s');
  setStatus(`正在请求预签名地址: ${file.name} ...`);
  const signed = await requestUploadUrl(file);

  setStatus('已拿到预签名地址，开始上传到 S3 ...');
  await uploadToS3(file, signed, (percent, loaded, total, nowMs) => {
    if (lastTickMs > 0 && nowMs > lastTickMs && loaded >= lastLoaded) {
      const deltaSec = (nowMs - lastTickMs) / 1000;
      const deltaBytes = loaded - lastLoaded;
      const instantRate = deltaSec > 0 ? deltaBytes / deltaSec : 0;
      // Exponential smoothing keeps the speed readable instead of jumping each event.
      smoothedRate = smoothedRate === 0 ? instantRate : smoothedRate * 0.7 + instantRate * 0.3;
    }

    lastLoaded = loaded;
    lastTickMs = nowMs;

    const rateText = formatRate(smoothedRate);
    setProgress(percent, rateText);
    setStatus(`上传中: ${Math.round(percent)}% · ${rateText} (${formatSize(loaded)} / ${formatSize(total)})`);
  });
  setProgress(100, '完成');

  setStatus([
    '上传成功',
    `bucket: ${signed.bucket}`,
    `key: ${signed.key}`,
    `location: ${signed.location}`,
    `expiresAt: ${signed.expiresAt}`
  ].join('\n'));
}

pickBtn.addEventListener('click', () => fileInput.click());
fileInput.addEventListener('change', async (event) => {
  const file = event.target.files && event.target.files[0];
  if (!file) return;
  try {
    await uploadFile(file);
  } catch (err) {
    setStatus(err.message || String(err), true);
  }
});

['dragenter', 'dragover'].forEach((evt) => {
  dropZone.addEventListener(evt, (e) => {
    e.preventDefault();
    e.stopPropagation();
    dropZone.classList.add('dragging');
  });
});

['dragleave', 'drop'].forEach((evt) => {
  dropZone.addEventListener(evt, (e) => {
    e.preventDefault();
    e.stopPropagation();
    dropZone.classList.remove('dragging');
  });
});

dropZone.addEventListener('drop', async (e) => {
  const file = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
  if (!file) {
    setStatus('未检测到文件', true);
    return;
  }

  try {
    await uploadFile(file);
  } catch (err) {
    setStatus(err.message || String(err), true);
  }
});

