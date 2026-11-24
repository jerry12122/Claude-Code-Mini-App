# 圖片傳送回執

`post_message` 可帶 `image_paths` 或目標 session 既有的 `attachment_ids`。純文字呼叫維持原本的 `ok`、`message_id`、`created_at`。

`image_paths` 每一項都必須已經在目標 session 的工作目錄，或該 session 的受控上傳區。呼叫端要先把工作區外的截圖複製進去。不接受 URL、base64、任意路徑、符號連結逃出，也不刪來源。內容必須能完整解碼成 png、jpeg 或 gif，單邊不超過 8192、總像素不超過 4096×4096，單張上限 8MB，單次最多 8 張。只靠檔頭、SVG、假副檔名，以及沒有解碼器的 webp，都拒絕。

成功時 `delivery` 為 `stored_verified`，並回 `session_id`、`message_id` 與依序的 `attachments`（`id`、`url`、`mime`、`bytes`、`sha256`）。回讀必須等於這次接受的位元組雜湊；同大小換成另一張圖不算成功。它不是畫面已載入，也不是使用者已讀。assistant 與 system 的圖片都走既有附件卡；工作室時間軸也顯示附件。

`room_ids` 只包含這則訊息實際出現在時間軸的工作室。沒有這個欄位，就是沒有出現在工作室時間軸。寫進 worker 或私人 session，不能當成使用者的工作室已經有圖。

沒有冪等鍵。錯誤且沒有 `message_id` 時沒有成功訊息，可以重試。已寫入但回讀或撤回失敗時，工具結果帶 `message_id` 與 `retry=do_not_resend`，不要重送。失敗不回 `ok: true`，不留下成功訊息，也不刪來源。撤回仍先守住訊息、佇列與其他存取引用。只有資料庫明確回「附件列已不在」時才刪這次的新檔；查詢失敗或無法判斷時保留檔案。

`sha256` 只放在這次 `post_message` 回執，對照的是這次接受的位元組。`get_messages` 的 attachments 維持 `id`、`url`、`mime`、`bytes`，不附 `sha256`，也不拿先前記住的雜湊當成重新驗證。畫面沿用既有附件渲染，不另外做載入或已讀回條。
