import type { APIRoute } from "astro";

export const GET: APIRoute = ({ site }) =>
	new Response(
		`<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n\t<url><loc>${new URL("/", site)}</loc></url>\n</urlset>\n`,
	);
