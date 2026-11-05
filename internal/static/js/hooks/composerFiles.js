let composerFilesDB = null;

function openComposerFiles() {
  if (!composerFilesDB) {
    composerFilesDB = new Promise((resolve, reject) => {
      let failed = false;
      const request = indexedDB.open('miniapp-composer-files', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('drafts');
      request.onerror = () => { failed = true; reject(request.error); };
      request.onblocked = () => { failed = true; reject(new Error('草稿儲存空間暫時無法開啟')); };
      request.onsuccess = () => {
        const database = request.result;
        if (failed) { database.close(); return; }
        database.onversionchange = () => { database.close(); composerFilesDB = null; };
        resolve(database);
      };
    }).catch(err => { composerFilesDB = null; throw err; });
  }
  return composerFilesDB;
}

async function readComposerFiles(key) {
  const database = await openComposerFiles();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction('drafts', 'readonly');
    const request = transaction.objectStore('drafts').get(key);
    transaction.oncomplete = () => resolve(request.result || []);
    transaction.onabort = () => reject(transaction.error || new Error('草稿讀取失敗'));
    transaction.onerror = () => {}; // onabort owns the error.
  });
}

async function writeComposerFiles(key, items) {
  // Blob URLs belong to one document; keep Files and regenerate previews on reload.
  const stored = items.map(({ previewUrl, ...item }) => ({ ...item,
    status: item.status === 'uploading' ? 'error' : item.status,
    error: item.status === 'uploading' ? '上傳已中斷，請重試' : item.error,
  }));
  const database = await openComposerFiles();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction('drafts', 'readwrite');
    const store = transaction.objectStore('drafts');
    if (stored.length) store.put(stored, key);
    else store.delete(key);
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error || new Error('附件草稿儲存失敗'));
    transaction.onerror = () => {};
  });
}

function restoreComposerFiles(items) {
  return items.map(item => ({ ...item,
    previewUrl: item.file?.type.startsWith('image/') ? URL.createObjectURL(item.file) : '',
  }));
}
