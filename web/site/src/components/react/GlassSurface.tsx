import { LazyMotion, domAnimation, MotionConfig } from "motion/react";
import * as m from "motion/react-m";
import type { ReactNode } from "react";
import type { HomeArticle, HomeProject } from "../../lib/home-data";
import { articleHref, projectDestination } from "../../lib/links";

type GlassSurfaceProps =
  | { kind: "article"; article: HomeArticle; headingLevel?: 2 | 3 }
  | { kind: "project"; project: HomeProject; headingLevel?: 2 | 3 };

function MotionStyle({ children }: { children: ReactNode }) {
  return (
    <MotionConfig reducedMotion="user">
      <LazyMotion features={domAnimation} strict>
        {children}
      </LazyMotion>
    </MotionConfig>
  );
}

export default function GlassSurface(props: GlassSurfaceProps) {
  if (props.kind === "article") {
    const href = articleHref(props.article);
    return (
      <MotionStyle>
        <m.article
          className="glass-surface card-surface article-card"
          tabIndex={-1}
          whileHover={{ y: -3, scale: 1.006 }}
          whileTap={{ scale: 0.99 }}
          transition={{ type: "spring", stiffness: 360, damping: 30, mass: 0.72 }}
        >
          <a className="card-link" href={href}>
            <div className="card__body">
              {props.article.published && (
                <p className="card__date">
                  <time dateTime={props.article.date_time}>{props.article.published}</time>
                </p>
              )}
              {props.headingLevel === 2 ? <h2>{props.article.title}</h2> : <h3>{props.article.title}</h3>}
              <p className="card__preview">{props.article.preview}</p>
            </div>
          </a>
        </m.article>
      </MotionStyle>
    );
  }

  const destination = projectDestination(props.project);
  return (
    <MotionStyle>
      <m.article
        className="glass-surface card-surface project-card"
        tabIndex={-1}
        whileHover={{ y: -3, scale: 1.006 }}
        whileTap={{ scale: 0.99 }}
        transition={{ type: "spring", stiffness: 360, damping: 30, mass: 0.72 }}
      >
        <a
          className="project-link"
          draggable={false}
          href={destination.href}
          target={destination.external ? "_blank" : undefined}
          rel={destination.external ? "noopener noreferrer" : undefined}
        >
          <div className="project-card__media">
            <img
              className="project-image"
              draggable={false}
              src={props.project.image_url}
              alt={props.project.image_alt}
              loading="lazy"
              decoding="async"
              width="1280"
              height="720"
            />
          </div>
          <div className="project-card__footer">
            {props.headingLevel === 2 ? <h2>{props.project.name}</h2> : <h3>{props.project.name}</h3>}
            {props.project.github_url && <span className="project-card__github">GitHub <span aria-hidden="true">↗</span></span>}
          </div>
        </a>
      </m.article>
    </MotionStyle>
  );
}
