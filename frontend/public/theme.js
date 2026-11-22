// Applies the saved theme before the first paint (loaded from <head>, so there is no flash). Light is the default.
try {
  if (localStorage.getItem('moipctl-theme') === 'dark') document.documentElement.classList.add('dark')
} catch (e) {
  /* storage can be blocked; stay light */
}
