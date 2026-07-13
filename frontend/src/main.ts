const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `<h1>Aux</h1><p id="status">checking backend…</p>`;

fetch("/api/health")
  .then((res) => res.json())
  .then((data: { status: string }) => {
    document.querySelector("#status")!.textContent = `backend: ${data.status}`;
  })
  .catch(() => {
    document.querySelector("#status")!.textContent = "backend: unreachable";
  });
