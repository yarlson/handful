import type { APIRoute } from "astro";
import { pages } from "../docs";

export const GET: APIRoute = ({ site }) => {
	const paths = ["/", "/security/", "/docs/", ...pages.map((page) => `/docs/${page.slug}/`)];
	const urls = paths.map((path) => `\t<url><loc>${new URL(path, site)}</loc></url>\n`).join("");
	return new Response(`<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}</urlset>\n`);
};
